// Command inari-ext-argocd is the Inari ArgoCD reference backend extension.
// It is launched and supervised by the control plane's Extension Host as a
// hashicorp go-plugin subprocess (plan §5.8); configuration arrives via
// environment variables:
//
//	INARI_AGENT_GATEWAY_ADDR     control plane Agent Gateway endpoint (required)
//	INARI_AGENT_GATEWAY_INSECURE "true" disables TLS (local development only)
//	INARI_AGENT_GATEWAY_TLS_NAME override TLS server name (optional)
//	INARI_MANAGED_PROJECTS       comma-separated ArgoCD AppProjects managed by
//	                             Inari (default "inari"); all actions are
//	                             scoped to these projects
//	INARI_ACTION_TIMEOUT         e.g. "60s"; per-action round-trip bound
//	INARI_LEGACY_GATEWAY_TOKEN   "true" opts into the deprecated shared
//	                             extension gate token (pre-W2 compat, off by
//	                             default); the token itself still comes from
//	                             INARI_EXTENSION_GATEWAY_TOKEN
package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/7K-Inari/inari-ext-argocd/internal/argocd"
	"github.com/7K-Inari/inari-ext-argocd/internal/extplugin"
	"github.com/7K-Inari/inari-ext-argocd/internal/gateway"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatalf("inari-ext-argocd: %v", err)
	}
}

func run(ctx context.Context) error {
	cfg := extplugin.Config{
		Scoping: argocd.Scoping{ManagedProjects: managedProjects()},
		Timeout: actionTimeout(),
	}

	// Fail closed by design: without a gateway address the plugin still
	// serves (so the host sees a healthy handshake and clear capability
	// metadata) but every action returns CodeUnavailable.
	if addr := os.Getenv("INARI_AGENT_GATEWAY_ADDR"); addr != "" {
		gw, err := gateway.Dial(gateway.Config{
			Addr:               addr,
			Insecure:           os.Getenv("INARI_AGENT_GATEWAY_INSECURE") == "true",
			TLSServerName:      os.Getenv("INARI_AGENT_GATEWAY_TLS_NAME"),
			LegacyToken:        legacyGateToken(),
			LegacyTokenEnabled: legacyGateTokenEnabled(),
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
	return p.Serve(ctx)
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
	if v := os.Getenv("INARI_ACTION_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
		log.Printf("invalid INARI_ACTION_TIMEOUT %q, using default", v)
	}
	return argocd.DefaultTimeout
}
