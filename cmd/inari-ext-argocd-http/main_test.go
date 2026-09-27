package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"

	"github.com/7K-Inari/inari-ext-argocd/internal/argocd"
	"github.com/7K-Inari/inari-ext-argocd/internal/extplugin"
	"github.com/7K-Inari/inari-ext-argocd/internal/gateway"
)

type fakeGateway struct {
	last gateway.Request
	ack  *agentv1.CommandAck
	err  error
}

func (f *fakeGateway) InvokeAction(_ context.Context, req gateway.Request) (*agentv1.CommandAck, error) {
	f.last = req
	return f.ack, f.err
}

func buildHandler(t *testing.T, gw gateway.Gateway) http.Handler {
	t.Helper()
	p, err := extplugin.Build(extplugin.Config{
		Gateway: gw,
		Scoping: argocd.Scoping{ManagedProjects: []string{"inari"}},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return actionsHandler(p)
}

func syncBody() string {
	return `{"clusterId":"cluster-1","app":{"name":"web-shop","namespace":"argocd","project":"inari"}}`
}

func TestActionsHandlerForwardsHostInjectedCredential(t *testing.T) {
	gw := &fakeGateway{ack: &agentv1.CommandAck{CommandId: "cmd-1", Result: agentv1.CommandResult_COMMAND_RESULT_APPLIED}}
	h := buildHandler(t, gw)
	r := httptest.NewRequest(http.MethodPost, "/actions/argocd.sync", strings.NewReader(syncBody()))
	r.Header.Set("X-Inari-User", "user-1")
	r.Header.Set("X-Inari-Org", "tenant-acme")
	r.Header.Set("X-Inari-Auth-Method", "oidc-sso-session")
	r.Header.Set("X-Inari-Downstream-Authorization", "Bearer sso-token-123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if gw.last.UserToken != "sso-token-123" {
		t.Fatalf("expected per-user token forwarded to gateway, got %q", gw.last.UserToken)
	}
	if gw.last.TenantID != "tenant-acme" {
		t.Fatalf("tenant binding: %q", gw.last.TenantID)
	}
}

func TestActionsHandlerFailsClosedWithoutSession(t *testing.T) {
	gw := &fakeGateway{ack: &agentv1.CommandAck{CommandId: "cmd-1", Result: agentv1.CommandResult_COMMAND_RESULT_APPLIED}}
	h := buildHandler(t, gw)
	r := httptest.NewRequest(http.MethodPost, "/actions/argocd.sync", strings.NewReader(syncBody()))
	r.Header.Set("X-Inari-User", "user-1")
	r.Header.Set("X-Inari-Org", "tenant-acme")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Inari-Reauth"); got != "argocd" {
		t.Fatalf("expected X-Inari-Reauth: argocd, got %q", got)
	}
	if !strings.Contains(w.Body.String(), `"code":"reauth_required"`) {
		t.Fatalf("expected typed reauth body, got %s", w.Body.String())
	}
	if gw.last.Command != nil {
		t.Fatal("gateway must not be called without a user session")
	}
}

func TestActionsHandlerSignalsReauthOnExpiredDownstreamSession(t *testing.T) {
	gw := &fakeGateway{ack: &agentv1.CommandAck{
		CommandId: "cmd-2",
		Result:    agentv1.CommandResult_COMMAND_RESULT_FAILED,
		Message:   argocd.ReauthSignalPrefix + ": argocd session expired (401)",
	}}
	h := buildHandler(t, gw)
	r := httptest.NewRequest(http.MethodPost, "/actions/argocd.sync", strings.NewReader(syncBody()))
	r.Header.Set("X-Inari-User", "user-1")
	r.Header.Set("X-Inari-Org", "tenant-acme")
	r.Header.Set("X-Inari-Auth-Method", "oidc-sso-session")
	r.Header.Set("X-Inari-Downstream-Authorization", "Bearer expired-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Inari-Reauth"); got != "argocd" {
		t.Fatalf("expected X-Inari-Reauth: argocd, got %q", got)
	}
	if !strings.Contains(w.Body.String(), `"code":"reauth_required"`) {
		t.Fatalf("expected typed reauth body, got %s", w.Body.String())
	}
}
