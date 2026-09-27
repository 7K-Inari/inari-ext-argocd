package extplugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"github.com/7K-Inari/inari-plugin-sdk/testkit"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"github.com/7K-Inari/inari-ext-argocd/internal/argocd"
	"github.com/7K-Inari/inari-ext-argocd/internal/gateway"
)

// fakeGateway records requests and returns a scripted ack or error.
type fakeGateway struct {
	last gateway.Request
	ack  *agentv1.CommandAck
	err  error
}

func (f *fakeGateway) InvokeAction(_ context.Context, req gateway.Request) (*agentv1.CommandAck, error) {
	f.last = req
	return f.ack, f.err
}

var testAuth = pluginsdk.AuthContext{PrincipalID: "user-1", TenantID: "tenant-acme"}
var scoping = argocd.Scoping{ManagedProjects: []string{"inari"}}

func build(t *testing.T, gw gateway.Gateway) *testkit.Client {
	t.Helper()
	p, err := Build(Config{Gateway: gw, Scoping: scoping})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return testkit.Run(t, p)
}

// withUserSession mimics the control plane's extension proxy injecting the
// per-user SSO session credential (oidc-sso-session) as connection metadata.
func withUserSession(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx,
		pluginsdk.HeaderAuthMethod, pluginsdk.AuthMethodOIDCSSOSession.String(),
		pluginsdk.HeaderDownstreamAuthorization, "Bearer "+token,
	)
}

func invoke(t *testing.T, c *testkit.Client, action string, payload any) (*pluginsdk.Response, error) {
	t.Helper()
	return invokeWithSession(t, c, action, payload, "user-token-1")
}

func invokeWithSession(t *testing.T, c *testkit.Client, action string, payload any, token string) (*pluginsdk.Response, error) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if token != "" {
		ctx = withUserSession(ctx, token)
	}
	return c.Invoke(ctx, action, testAuth, b)
}

func TestCapabilities(t *testing.T) {
	c := build(t, &fakeGateway{})
	ctx := context.Background()
	info, err := c.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.GetName() != "inari-ext-argocd" {
		t.Fatalf("name: %v", info.GetName())
	}
	actions, err := c.Capabilities(ctx)
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	want := []string{argocd.ActionRefresh, argocd.ActionResourceAction, argocd.ActionRollback, argocd.ActionSync}
	if len(actions) != len(want) {
		t.Fatalf("actions: %v", actions)
	}
	for i, a := range actions {
		if a.GetName() != want[i] {
			t.Fatalf("action %d: got %q want %q", i, a.GetName(), want[i])
		}
		if len(a.GetInputSchema()) == 0 || len(a.GetOutputSchema()) == 0 {
			t.Fatalf("action %q missing schemas", a.GetName())
		}
	}
}

func TestDeclaresOIDCSSOSessionAuthMethod(t *testing.T) {
	p, err := Build(Config{Gateway: &fakeGateway{}, Scoping: scoping})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	info, err := pluginsdk.NewGRPCService(p).GetInfo(context.Background(), &pluginv1.GetInfoRequest{})
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	methods := info.GetAuthMethods()
	if len(methods) != 1 {
		t.Fatalf("expected exactly one declared auth method, got %v", methods)
	}
	m := methods[0]
	if m.GetType() != pluginv1.AuthMethod_TYPE_OIDC_SSO_SESSION {
		t.Fatalf("auth method type: %v", m.GetType())
	}
	if m.GetAudience() != "argocd" {
		t.Fatalf("audience (session provider key): %q", m.GetAudience())
	}
	if !m.GetIsDefault() {
		t.Fatal("oidc-sso-session must be the default auth method")
	}
}

func TestFailsClosedWithoutUserSession(t *testing.T) {
	gw := &fakeGateway{ack: &agentv1.CommandAck{CommandId: "cmd-1", Result: agentv1.CommandResult_COMMAND_RESULT_APPLIED}}
	c := build(t, gw)
	_, err := invokeWithSession(t, c, argocd.ActionSync, map[string]any{
		"clusterId": "cluster-1",
		"app":       map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
	}, "")
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != pluginsdk.CodeUnauthenticated {
		t.Fatalf("expected CodeUnauthenticated without a user session, got %v", err)
	}
	if gw.last.Command != nil {
		t.Fatal("gateway must not be called without a user session")
	}
}

func TestForwardsUserTokenAsMetadataOnly(t *testing.T) {
	gw := &fakeGateway{ack: &agentv1.CommandAck{CommandId: "cmd-1", Result: agentv1.CommandResult_COMMAND_RESULT_APPLIED}}
	c := build(t, gw)
	const token = "sso-session-token-xyz"
	_, err := invokeWithSession(t, c, argocd.ActionSync, map[string]any{
		"clusterId": "cluster-1",
		"app":       map[string]any{"name": "web-shop", "namespace": "argocd", "project": "inari"},
	}, token)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if gw.last.UserToken != token {
		t.Fatalf("expected raw user token forwarded to gateway, got %q", gw.last.UserToken)
	}
	raw, err := proto.Marshal(gw.last.Command)
	if err != nil {
		t.Fatalf("marshal command: %v", err)
	}
	if json.Valid(raw) && false {
		t.Fatal("unreachable")
	}
	if contains(raw, token) {
		t.Fatal("user token leaked into the command payload")
	}
	if gw.last.Command.GetUserCredentialRef() != "" {
		t.Fatalf("extension must not self-assert user_credential_ref, got %q", gw.last.Command.GetUserCredentialRef())
	}
}

func contains(raw []byte, s string) bool {
	return len(s) > 0 && len(raw) >= len(s) && string(raw) != "" && indexOf(raw, []byte(s)) >= 0
}

func indexOf(haystack, needle []byte) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func TestExpiredDownstreamSessionSignalsReauth(t *testing.T) {
	gw := &fakeGateway{ack: &agentv1.CommandAck{
		CommandId: "cmd-2",
		Result:    agentv1.CommandResult_COMMAND_RESULT_FAILED,
		Message:   argocd.ReauthSignalPrefix + ": argocd session expired (401)",
	}}
	c := build(t, gw)
	_, err := invoke(t, c, argocd.ActionSync, map[string]any{
		"clusterId": "cluster-1",
		"app":       map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
	})
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != pluginsdk.CodeUnauthenticated {
		t.Fatalf("expected CodeUnauthenticated for expired downstream session, got %v", err)
	}
	if pe.Details["code"] != "reauth_required" || pe.Details["provider"] != "argocd" {
		t.Fatalf("expected typed re-auth details, got %v", pe.Details)
	}
}

func TestSyncRoundTrip(t *testing.T) {
	gw := &fakeGateway{ack: &agentv1.CommandAck{
		CommandId: "cmd-1",
		Result:    agentv1.CommandResult_COMMAND_RESULT_APPLIED,
		Message:   "synced",
	}}
	c := build(t, gw)
	resp, err := invoke(t, c, argocd.ActionSync, map[string]any{
		"clusterId": "cluster-1",
		"app":       map[string]any{"name": "web-shop", "namespace": "argocd", "project": "inari"},
		"prune":     true,
	})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var res Result
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatalf("result: %v", err)
	}
	if res.Outcome != "applied" || res.Message != "synced" {
		t.Fatalf("result: %+v", res)
	}
	// Tenant identity must come from the AuthContext, never from the payload.
	if gw.last.TenantID != "tenant-acme" || gw.last.ClusterID != "cluster-1" {
		t.Fatalf("routing: %+v", gw.last)
	}
	if gw.last.Command.GetAction() != "sync" { // agent-side verb is bare

		t.Fatalf("command action: %v", gw.last.Command.GetAction())
	}
	if gw.last.Command.GetParameters().AsMap()["prune"] != true {
		t.Fatalf("params not forwarded: %v", gw.last.Command.GetParameters().AsMap())
	}
}

func TestFailsClosedWithoutGateway(t *testing.T) {
	c := build(t, nil)
	_, err := invoke(t, c, argocd.ActionSync, map[string]any{
		"clusterId": "cluster-1",
		"app":       map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
	})
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != pluginsdk.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable, got %v", err)
	}
}

func TestFailsClosedWhenGatewayErrors(t *testing.T) {
	c := build(t, &fakeGateway{err: errors.New("agent disconnected")})
	_, err := invoke(t, c, argocd.ActionRefresh, map[string]any{
		"clusterId": "cluster-1",
		"app":       map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
	})
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != pluginsdk.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable, got %v", err)
	}
}

func TestRejectsUnmanagedProject(t *testing.T) {
	gw := &fakeGateway{}
	c := build(t, gw)
	_, err := invoke(t, c, argocd.ActionSync, map[string]any{
		"clusterId": "cluster-1",
		"app":       map[string]any{"name": "a", "namespace": "argocd", "project": "default"},
	})
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != pluginsdk.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument, got %v", err)
	}
	if gw.last.Command != nil {
		t.Fatal("gateway must not be called for unmanaged projects")
	}
}

func TestRejectsUnknownFields(t *testing.T) {
	c := build(t, &fakeGateway{})
	_, err := invoke(t, c, argocd.ActionSync, map[string]any{
		"clusterId": "cluster-1",
		"app":       map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
		"tenantId":  "tenant-evil",
	})
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != pluginsdk.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument, got %v", err)
	}
}

func TestAgentFailureSurfaces(t *testing.T) {
	c := build(t, &fakeGateway{ack: &agentv1.CommandAck{
		CommandId: "cmd-9",
		Result:    agentv1.CommandResult_COMMAND_RESULT_FAILED,
		Message:   "application not found",
	}})
	_, err := invoke(t, c, argocd.ActionRollback, map[string]any{
		"clusterId":  "cluster-1",
		"app":        map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
		"revisionId": 2,
	})
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != pluginsdk.CodeInternal {
		t.Fatalf("expected CodeInternal, got %v", err)
	}
}

func TestHealth(t *testing.T) {
	ok, _, err := build(t, &fakeGateway{}).Health(context.Background())
	if err != nil || !ok {
		t.Fatalf("with gateway: serving=%v err=%v", ok, err)
	}
	ok, msg, err := build(t, nil).Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if ok || msg == "" {
		t.Fatalf("without gateway should not serve: %v %q", ok, msg)
	}
}
