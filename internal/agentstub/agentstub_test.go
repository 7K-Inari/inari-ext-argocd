package agentstub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
)

type recordedCall struct {
	method string
	url    string
	body   string
	auth   string
}

type recordingServer struct {
	mu     sync.Mutex
	calls  []recordedCall
	status int
	delay  time.Duration
}

func (r *recordingServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.calls = append(r.calls, recordedCall{
			method: req.Method,
			url:    req.URL.String(),
			body:   string(b),
			auth:   req.Header.Get("authorization"),
		})
		r.mu.Unlock()
		if r.delay > 0 {
			time.Sleep(r.delay)
		}
		if r.status != 0 {
			w.WriteHeader(r.status)
		}
		_, _ = w.Write([]byte(`{}`))
	})
}

func (r *recordingServer) last() recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[len(r.calls)-1]
}

func cmd(t *testing.T, action string, params map[string]any) *agentv1.InvokeAction {
	t.Helper()
	p, err := structpb.NewStruct(params)
	if err != nil {
		t.Fatalf("struct: %v", err)
	}
	return &agentv1.InvokeAction{CommandId: "c1", Action: action, Parameters: p}
}

func managedAppParams() map[string]any {
	return map[string]any{"app": map[string]any{"name": "web-shop", "namespace": "argocd", "project": "inari"}}
}

func managedClient(srv *httptest.Server) *ArgoCDClient {
	return &ArgoCDClient{BaseURL: srv.URL, BearerToken: "tok", ManagedProjects: []string{"inari"}}
}

func TestSyncHitsArgoCDWireShape(t *testing.T) {
	rec := &recordingServer{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	p := managedAppParams()
	p["prune"] = true
	p["strategy"] = "hook"
	ack := managedClient(srv).Execute(context.Background(), cmd(t, "argocd.sync", p))
	if ack.GetResult() != agentv1.CommandResult_COMMAND_RESULT_APPLIED {
		t.Fatalf("ack: %v", ack)
	}
	call := rec.last()
	if call.method != http.MethodPost || call.url != "/api/v1/applications/web-shop/sync" {
		t.Fatalf("call: %+v", call)
	}
	if call.auth != "Bearer tok" {
		t.Fatalf("auth header missing: %+v", call)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(call.body), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["prune"] != true {
		t.Fatalf("prune missing: %v", body)
	}
	if _, ok := body["strategy"].(map[string]any)["hook"]; !ok {
		t.Fatalf("strategy shape wrong (ArgoCD expects {\"hook\":{}}): %v", body)
	}
}

func TestRefreshHardQueryParam(t *testing.T) {
	rec := &recordingServer{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	p := managedAppParams()
	p["hard"] = true
	ack := managedClient(srv).Execute(context.Background(), cmd(t, "argocd.refresh", p))
	if ack.GetResult() != agentv1.CommandResult_COMMAND_RESULT_APPLIED {
		t.Fatalf("ack: %v", ack)
	}
	if rec.last().url != "/api/v1/applications/web-shop?refresh=hard" {
		t.Fatalf("url: %s", rec.last().url)
	}
}

func TestRollbackBodyCarriesHistoryID(t *testing.T) {
	rec := &recordingServer{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	p := managedAppParams()
	p["revisionId"] = 7
	ack := managedClient(srv).Execute(context.Background(), cmd(t, "argocd.rollback", p))
	if ack.GetResult() != agentv1.CommandResult_COMMAND_RESULT_APPLIED {
		t.Fatalf("ack: %v", ack)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(rec.last().body), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["id"] != float64(7) {
		t.Fatalf("id: %v", body)
	}
}

func TestResourceActionBodyIsBareActionName(t *testing.T) {
	rec := &recordingServer{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	p := managedAppParams()
	p["resource"] = map[string]any{"group": "apps", "version": "v1", "kind": "Deployment", "name": "web", "namespace": "shop"}
	p["resourceAction"] = "restart"
	ack := managedClient(srv).Execute(context.Background(), cmd(t, "argocd.resource-action", p))
	if ack.GetResult() != agentv1.CommandResult_COMMAND_RESULT_APPLIED {
		t.Fatalf("ack: %v", ack)
	}
	call := rec.last()
	// ArgoCD expects the request body to be the action name as a JSON string.
	var action string
	if err := json.Unmarshal([]byte(call.body), &action); err != nil || action != "restart" {
		t.Fatalf("body must be the bare action name string, got %q", call.body)
	}
	for _, want := range []string{"group=apps", "version=v1", "kind=Deployment", "resourceName=web", "namespace=shop"} {
		if !strings.Contains(call.url, want) {
			t.Fatalf("url %q missing %q", call.url, want)
		}
	}
}

func TestUnmanagedProjectFailsClosed(t *testing.T) {
	rec := &recordingServer{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	p := map[string]any{"app": map[string]any{"name": "x", "namespace": "argocd", "project": "default"}}
	ack := managedClient(srv).Execute(context.Background(), cmd(t, "argocd.sync", p))
	if ack.GetResult() != agentv1.CommandResult_COMMAND_RESULT_FAILED {
		t.Fatalf("expected FAILED, got %v", ack.GetResult())
	}
	if len(rec.calls) != 0 {
		t.Fatalf("ArgoCD must not be touched for unmanaged projects; calls: %+v", rec.calls)
	}
}

func TestArgoCDErrorBecomesFailedAck(t *testing.T) {
	rec := &recordingServer{status: http.StatusConflict}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	ack := managedClient(srv).Execute(context.Background(), cmd(t, "argocd.sync", managedAppParams()))
	if ack.GetResult() != agentv1.CommandResult_COMMAND_RESULT_FAILED {
		t.Fatalf("expected FAILED, got %v", ack.GetResult())
	}
	if !strings.Contains(ack.GetMessage(), "409") {
		t.Fatalf("message should carry the status: %q", ack.GetMessage())
	}
}

func TestCommandTimeoutBoundsExecution(t *testing.T) {
	rec := &recordingServer{delay: 2 * time.Second}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	c := cmd(t, "argocd.sync", managedAppParams())
	c.Timeout = durationpb.New(100 * time.Millisecond)
	start := time.Now()
	ack := managedClient(srv).Execute(context.Background(), c)
	if ack.GetResult() != agentv1.CommandResult_COMMAND_RESULT_FAILED {
		t.Fatalf("expected FAILED on timeout, got %v", ack.GetResult())
	}
	if time.Since(start) > time.Second {
		t.Fatalf("timeout not honored: %s", time.Since(start))
	}
}

func TestAppNameIsPathEscaped(t *testing.T) {
	rec := &recordingServer{}
	srv := httptest.NewServer(rec.handler())
	defer srv.Close()

	// An app name containing a slash must not alter the path structure.
	p := map[string]any{"app": map[string]any{"name": "a/b", "namespace": "argocd", "project": "inari"}}
	managedClient(srv).Execute(context.Background(), cmd(t, "argocd.sync", p))
	if rec.last().url != "/api/v1/applications/a%2Fb/sync" {
		t.Fatalf("url: %s", rec.last().url)
	}
}
