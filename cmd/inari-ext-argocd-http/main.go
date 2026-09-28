// Command inari-ext-argocd-http serves the ArgoCD extension in dial mode
// (plan §5.8): the pluginv1 contract over ConnectRPC (HTTP) plus the
// POST /actions/<name> surface the control-plane extension proxy forwards
// (/api/extensions/<name>/actions/<action> → /actions/<action>, with the
// authenticated identity in X-Inari-User / X-Inari-Org).
//
// The sibling command (cmd/inari-ext-argocd) speaks the go-plugin subprocess
// protocol for host-supervised exec mode; this binary runs the extension as a
// standalone network service so the Extension Host's dial mode (registered
// endpoint + VerifyHandshake + reverse proxy) can reach it.
//
// Environment mirrors the subprocess command, plus:
//
//	INARI_HTTP_ADDR   listen address (default :8080)
package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	"github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1/pluginv1connect"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"google.golang.org/grpc/metadata"

	"github.com/7K-Inari/inari-ext-argocd/internal/argocd"
	"github.com/7K-Inari/inari-ext-argocd/internal/extplugin"
	"github.com/7K-Inari/inari-ext-argocd/internal/gateway"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("inari-ext-argocd-http: %v", err)
	}
}

func run() error {
	cfg := extplugin.Config{
		Scoping: argocd.Scoping{ManagedProjects: managedProjects()},
		Timeout: actionTimeout(),
	}
	if addr := os.Getenv("INARI_AGENT_GATEWAY_ADDR"); addr != "" {
		gw, err := gateway.Dial(gateway.Config{
			Addr:               addr,
			Insecure:           os.Getenv("INARI_AGENT_GATEWAY_INSECURE") == "true",
			TLSServerName:      os.Getenv("INARI_AGENT_GATEWAY_TLS_NAME"),
			LegacyToken:        legacyGateToken(),
			LegacyTokenEnabled: legacyGateTokenEnabled(),
			TunnelTokenURL:     os.Getenv("INARI_EXTENSION_TOKEN_URL"),
			TunnelClientID:     os.Getenv("INARI_EXTENSION_CLIENT_ID"),
			TunnelClientSecret: os.Getenv("INARI_EXTENSION_CLIENT_SECRET"),
		})
		if err != nil {
			return err
		}
		cfg.Gateway = gw
	} else {
		log.Print("INARI_AGENT_GATEWAY_ADDR unset: actions will fail closed with CodeUnavailable")
	}

	p, err := extplugin.Build(cfg)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	// Contract surface (Verify handshake, health, capabilities).
	path, handler := pluginv1connect.NewPluginContractServiceHandler(connectAdapter{s: pluginsdk.NewGRPCService(p)})
	mux.Handle(path, handler)
	// Proxy surface: POST /actions/<action>.
	mux.HandleFunc("/actions/", actionsHandler(p))

	addr := os.Getenv("INARI_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("inari-ext-argocd-http: dial mode listening on %s (contract at %s)", addr, path)
	return http.ListenAndServe(addr, mux) //nolint:gosec // test/dev server; platform deployments sit behind the control-plane proxy
}

// connectAdapter bridges the SDK's gRPC-shaped service to the ConnectRPC
// handler interface (same messages, connect.Request/Response wrappers).
type connectAdapter struct {
	s pluginv1.PluginContractServiceServer
}

func (a connectAdapter) GetInfo(ctx context.Context, req *connect.Request[pluginv1.GetInfoRequest]) (*connect.Response[pluginv1.GetInfoResponse], error) {
	r, err := a.s.GetInfo(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	// COMPAT (inari-server <= 2.0.0): the released control plane compares
	// api_version against the go-plugin wire version "1" instead of the
	// contract version (fixed by 7K-Inari/inari-server cf4ed87). Remove this
	// override once the platform runs the fixed server.
	if v := os.Getenv("INARI_COMPAT_API_VERSION"); v != "" && r.GetInfo() != nil {
		r.Info.ApiVersion = v
	}
	return connect.NewResponse(r), nil
}

func (a connectAdapter) GetCapabilities(ctx context.Context, req *connect.Request[pluginv1.GetCapabilitiesRequest]) (*connect.Response[pluginv1.GetCapabilitiesResponse], error) {
	r, err := a.s.GetCapabilities(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(r), nil
}

func (a connectAdapter) Invoke(ctx context.Context, req *connect.Request[pluginv1.InvokeRequest]) (*connect.Response[pluginv1.InvokeResponse], error) {
	r, err := a.s.Invoke(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(r), nil
}

func (a connectAdapter) HealthCheck(ctx context.Context, req *connect.Request[pluginv1.HealthCheckRequest]) (*connect.Response[pluginv1.HealthCheckResponse], error) {
	r, err := a.s.HealthCheck(ctx, req.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(r), nil
}

// Host-injected headers carrying the resolved per-user downstream
// credential (oidc-sso-session). Set only by the control plane's extension
// proxy (strip-then-inject); mirrored here into the contract's connection
// metadata. Values are credentials: never log them.
const (
	headerAuthMethod              = "X-Inari-Auth-Method"
	headerDownstreamAuthorization = "X-Inari-Downstream-Authorization"
	// headerReauth signals which third-party provider the user must
	// re-authenticate with (oidc-sso-session bootstrap/expiry) — same
	// contract as the control plane's extension proxy.
	headerReauth = "X-Inari-Reauth"
)

// actionsHandler maps the proxy-forwarded POST /actions/<action> onto the
// contract's Invoke, lifting identity from the injected headers (never from
// the payload — §5.8).
func actionsHandler(p *pluginsdk.Plugin) http.HandlerFunc {
	s := pluginsdk.NewGRPCService(p)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"detail":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		action := strings.TrimPrefix(r.URL.Path, "/actions/")
		if action == "" || strings.Contains(action, "/") {
			http.Error(w, `{"detail":"action name required"}`, http.StatusBadRequest)
			return
		}
		payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, `{"detail":"read body"}`, http.StatusBadRequest)
			return
		}
		if len(payload) == 0 {
			payload = []byte("{}")
		}
		ctx := r.Context()
		// Forward the host-injected downstream credential as connection
		// metadata (the SDK enforces the declared auth method fail-closed).
		if v := r.Header.Get(headerAuthMethod); v != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, pluginsdk.HeaderAuthMethod, v)
		}
		if v := r.Header.Get(headerDownstreamAuthorization); v != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, pluginsdk.HeaderDownstreamAuthorization, v)
		}
		// The contract service is invoked in-process here: bridge the
		// outgoing metadata to incoming so the SDK sees the connection
		// metadata exactly as it would over the wire.
		if md, ok := metadata.FromOutgoingContext(ctx); ok {
			ctx = metadata.NewIncomingContext(ctx, md)
		}
		resp, err := s.Invoke(ctx, &pluginv1.InvokeRequest{
			Action: action,
			AuthContext: &pluginv1.AuthContext{
				PrincipalId: r.Header.Get("X-Inari-User"),
				TenantId:    r.Header.Get("X-Inari-Org"),
			},
			Payload:   payload,
			RequestId: r.Header.Get("X-Request-Id"),
		})
		if err != nil {
			http.Error(w, `{"detail":"invoke failed"}`, http.StatusBadGateway)
			return
		}
		if pe := resp.GetError(); pe != nil {
			// Typed re-auth/session-expired signal: byte-identical shape to
			// the control plane's extension proxy so the UI's zero-prompt
			// SSO bootstrap + single retry works uniformly in dial mode.
			if pe.GetCode() == pluginv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED {
				w.Header().Set(headerReauth, argocd.SSOProvider)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"detail":"re-authentication required","code":"` + argocd.ReauthSignalPrefix + `"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(httpStatusFor(pe.GetCode()))
			_, _ = w.Write([]byte(`{"detail":"` + jsonEscape(pe.GetMessage()) + `","code":"` + pe.GetCode().String() + `"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp.GetResult())
	}
}

func httpStatusFor(c pluginv1.ErrorCode) int {
	switch c {
	case pluginv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT:
		return http.StatusBadRequest
	case pluginv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED:
		return http.StatusUnauthorized
	case pluginv1.ErrorCode_ERROR_CODE_PERMISSION_DENIED:
		return http.StatusForbidden
	case pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND:
		return http.StatusNotFound
	case pluginv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION:
		return http.StatusPreconditionFailed
	case pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func jsonEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(s)
}

// legacyGateToken returns the deprecated shared extension-gateway gate token
// (pre-W2 control planes). Inert unless legacyGateTokenEnabled is true.
func legacyGateToken() string { return os.Getenv("INARI_EXTENSION_GATEWAY_TOKEN") }

// legacyGateTokenEnabled reports whether the deprecated shared gate token
// path was explicitly opted into (INARI_LEGACY_GATEWAY_TOKEN=true). Default
// off: per-user OIDC SSO sessions are the supported auth path and there is
// no silent fallback to shared credentials.
func legacyGateTokenEnabled() bool {
	if os.Getenv("INARI_LEGACY_GATEWAY_TOKEN") != "true" {
		return false
	}
	if os.Getenv("INARI_EXTENSION_GATEWAY_TOKEN") != "" {
		log.Print("WARNING: INARI_LEGACY_GATEWAY_TOKEN=true: using the deprecated shared extension gate token (pre-W2 compatibility); migrate the control plane to per-user OIDC SSO sessions")
	}
	return true
}

func managedProjects() []string {
	raw := os.Getenv("INARI_MANAGED_PROJECTS")
	if raw == "" {
		return []string{"inari"}
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func actionTimeout() time.Duration {
	if raw := os.Getenv("INARI_ACTION_TIMEOUT"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			return d
		}
	}
	return 0
}
