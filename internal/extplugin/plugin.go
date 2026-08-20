// Package extplugin wires the ArgoCD actions into an inari-plugin-sdk Plugin:
// payload decoding, tenant binding from the authenticated AuthContext,
// validation + scoping (internal/argocd), and delivery through the Agent
// Gateway (internal/gateway). It fails closed: without a reachable gateway
// every action returns CodeUnavailable.
package extplugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"

	"github.com/7K-Inari/inari-ext-argocd/internal/argocd"
	"github.com/7K-Inari/inari-ext-argocd/internal/gateway"
)

// Info is the plugin identity reported to the host during handshake.
var Info = pluginsdk.Info{Name: "inari-ext-argocd", Version: Version}

// Config configures the plugin.
type Config struct {
	// Gateway is the Agent Gateway client. Nil fails closed: all actions
	// return CodeUnavailable.
	Gateway gateway.Gateway
	// Scoping restricts which ArgoCD projects the extension may touch.
	Scoping argocd.Scoping
	// Timeout bounds one round-trip through the agent. Defaults to
	// argocd.DefaultTimeout.
	Timeout time.Duration
}

func (c Config) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return argocd.DefaultTimeout
}

// Result is the JSON result returned by every action.
type Result struct {
	CommandID string `json:"commandId"`
	// Outcome is "applied", "accepted" or "failed" per the agent's CommandAck.
	Outcome string `json:"outcome"`
	Message string `json:"message,omitempty"`
}

// Build creates the plugin with all ArgoCD actions registered.
func Build(cfg Config) (*pluginsdk.Plugin, error) {
	p := pluginsdk.New(Info, pluginsdk.WithHealthChecker(health{cfg.Gateway}))
	for _, a := range []pluginsdk.Action{
		{
			Name:         argocd.ActionSync,
			Description:  "Sync an Inari-managed ArgoCD Application via the tenant-local ArgoCD API.",
			InputSchema:  schema("sync"),
			OutputSchema: resultSchema,
			Handler:      handle(cfg, argocd.ActionSync),
		},
		{
			Name:         argocd.ActionRefresh,
			Description:  "Refresh (optionally hard) an Inari-managed ArgoCD Application.",
			InputSchema:  schema("refresh"),
			OutputSchema: resultSchema,
			Handler:      handle(cfg, argocd.ActionRefresh),
		},
		{
			Name:         argocd.ActionRollback,
			Description:  "Roll back an Inari-managed ArgoCD Application to a deployment history ID.",
			InputSchema:  schema("rollback"),
			OutputSchema: resultSchema,
			Handler:      handle(cfg, argocd.ActionRollback),
		},
		{
			Name:         argocd.ActionResourceAction,
			Description:  "Run a built-in or custom Lua resource action on a resource of an Inari-managed Application.",
			InputSchema:  schema("resource-action"),
			OutputSchema: resultSchema,
			Handler:      handle(cfg, argocd.ActionResourceAction),
		},
	} {
		if err := p.RegisterAction(a); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// handle returns the Handler for one action: decode → tenant-bind → validate
// → tunnel → map the acknowledgement.
func handle(cfg Config, action string) pluginsdk.Handler {
	return func(ctx context.Context, req *pluginsdk.Request) (*pluginsdk.Response, error) {
		if cfg.Gateway == nil {
			return nil, pluginsdk.Errorf(pluginsdk.CodeUnavailable, "agent gateway is not configured; failing closed")
		}
		ac, ok := pluginsdk.AuthContextFrom(ctx)
		if !ok {
			return nil, pluginsdk.Errorf(pluginsdk.CodeUnauthenticated, "missing auth context")
		}

		// The tunnel correlates acks by command ID; never send an empty one
		// (some hosts/testkit leave RequestID unset).
		commandID := req.RequestID
		if commandID == "" {
			commandID = newCommandID()
		}

		var clusterID string
		var cmd *agentv1.InvokeAction
		var err error
		switch action {
		case argocd.ActionSync:
			var in argocd.SyncInput
			if err = decodeStrict(req.Payload, &in); err == nil {
				clusterID = in.ClusterID
				cmd, err = argocd.BuildSync(commandID, in, cfg.Scoping, cfg.timeout())
			}
		case argocd.ActionRefresh:
			var in argocd.RefreshInput
			if err = decodeStrict(req.Payload, &in); err == nil {
				clusterID = in.ClusterID
				cmd, err = argocd.BuildRefresh(commandID, in, cfg.Scoping, cfg.timeout())
			}
		case argocd.ActionRollback:
			var in argocd.RollbackInput
			if err = decodeStrict(req.Payload, &in); err == nil {
				clusterID = in.ClusterID
				cmd, err = argocd.BuildRollback(commandID, in, cfg.Scoping, cfg.timeout())
			}
		case argocd.ActionResourceAction:
			var in argocd.ResourceActionInput
			if err = decodeStrict(req.Payload, &in); err == nil {
				clusterID = in.ClusterID
				cmd, err = argocd.BuildResourceAction(commandID, in, cfg.Scoping, cfg.timeout())
			}
		default:
			return nil, pluginsdk.Errorf(pluginsdk.CodeNotFound, "unknown action %q", action)
		}
		if err != nil {
			return nil, pluginsdk.Errorf(pluginsdk.CodeInvalidArgument, "%s: %v", action, err)
		}

		callCtx, cancel := context.WithTimeout(ctx, cfg.timeout()+10*time.Second)
		defer cancel()
		ack, err := cfg.Gateway.InvokeAction(callCtx, gateway.Request{
			TenantID:  ac.TenantID,
			ClusterID: clusterID,
			Command:   cmd,
		})
		if err != nil {
			// Gateway unreachable or agent disconnected: fail closed.
			return nil, pluginsdk.Errorf(pluginsdk.CodeUnavailable, "agent gateway invoke failed: %v", err)
		}
		res := Result{CommandID: ack.GetCommandId(), Message: ack.GetMessage()}
		switch ack.GetResult() {
		case agentv1.CommandResult_COMMAND_RESULT_APPLIED:
			res.Outcome = "applied"
		case agentv1.CommandResult_COMMAND_RESULT_ACCEPTED:
			res.Outcome = "accepted"
		default:
			return nil, pluginsdk.Errorf(pluginsdk.CodeInternal, "%s failed on agent: %s", action, ack.GetMessage())
		}
		out, err := json.Marshal(res)
		if err != nil {
			return nil, pluginsdk.Errorf(pluginsdk.CodeInternal, "encode result: %v", err)
		}
		return &pluginsdk.Response{Result: out}, nil
	}
}

// decodeStrict decodes the payload rejecting unknown fields, so callers get
// an error instead of silently ignored typos.
func decodeStrict(payload []byte, v any) error {
	dec := json.NewDecoder(bytesReader(payload))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// newCommandID generates a random command ID for ack correlation when the
// host did not supply a request ID.
func newCommandID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
