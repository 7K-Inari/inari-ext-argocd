// Package e2e proves the full imperative-op round trip (plan §5.3):
//
//	plugin (testkit) → Agent Gateway (harness) → agent session →
//	tenant-local ArgoCD API (fake) → CommandAck → plugin result
//
// It runs in-process with bufconn and httptest, so `go test ./e2e` works
// without Docker. For a real-cluster variant see e2e/kind (kind + ArgoCD +
// cmd/inari-agentstub).
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"github.com/7K-Inari/inari-plugin-sdk/testkit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/7K-Inari/inari-ext-argocd/e2e/harness"
	"github.com/7K-Inari/inari-ext-argocd/internal/agentstub"
	"github.com/7K-Inari/inari-ext-argocd/internal/argocd"
	"github.com/7K-Inari/inari-ext-argocd/internal/extplugin"
	"github.com/7K-Inari/inari-ext-argocd/internal/gateway"
)

// fakeArgoCD records requests and serves minimal Application endpoints.
type fakeArgoCD struct {
	mu   sync.Mutex
	hits []string
}

func (f *fakeArgoCD) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits = append(f.hits, r.Method+" "+r.URL.String())
		f.mu.Unlock()
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	return mux
}

func (f *fakeArgoCD) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.hits...)
}

func startStack(t *testing.T) (*testkit.Client, *fakeArgoCD, *harness.Server, func()) {
	t.Helper()
	fake := &fakeArgoCD{}
	argoSrv := httptest.NewServer(fake.handler())
	t.Cleanup(argoSrv.Close)

	gwSrv := harness.NewServer()
	lis := bufconn.Listen(1024 * 1024)
	s := grpc.NewServer()
	desc := gwSrv.ServiceDesc()
	s.RegisterService(&desc, gwSrv)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sess, _ := gwSrv.RegisterAgent("cluster-1")
	argo := &agentstub.ArgoCDClient{BaseURL: argoSrv.URL, ManagedProjects: []string{"inari"}}
	go harness.RunAgent(ctx, sess, argo.Execute)

	p, err := extplugin.Build(extplugin.Config{
		Gateway: gateway.New(conn),
		Scoping: scopingForTest(),
	})
	if err != nil {
		t.Fatalf("build plugin: %v", err)
	}
	return testkit.Run(t, p), fake, gwSrv, cancel
}

func scopingForTest() argocd.Scoping { return argocd.Scoping{ManagedProjects: []string{"inari"}} }

func TestEndToEndRoundTrip(t *testing.T) {
	c, fake, _, _ := startStack(t)
	auth := pluginsdk.AuthContext{PrincipalID: "dev-1", TenantID: "tenant-acme"}

	payload := func(m map[string]any) []byte {
		b, _ := json.Marshal(m)
		return b
	}
	app := map[string]any{"name": "web-shop", "namespace": "argocd", "project": "inari"}

	steps := []struct {
		action  string
		payload map[string]any
		wantHit string
	}{
		{argocd.ActionSync, map[string]any{"clusterId": "cluster-1", "app": app, "prune": true}, "POST /api/v1/applications/web-shop/sync"},
		{argocd.ActionRefresh, map[string]any{"clusterId": "cluster-1", "app": app, "hard": true}, "GET /api/v1/applications/web-shop?refresh=hard"},
		{argocd.ActionRollback, map[string]any{"clusterId": "cluster-1", "app": app, "revisionId": 3}, "POST /api/v1/applications/web-shop/rollback"},
		{argocd.ActionResourceAction, map[string]any{
			"clusterId": "cluster-1", "app": app,
			"resource": map[string]any{"group": "apps", "kind": "Deployment", "name": "web", "namespace": "shop"},
			"action":   "restart",
		}, "POST /api/v1/applications/web-shop/resource/actions?"},
	}

	for _, step := range steps {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		resp, err := c.Invoke(ctx, step.action, auth, payload(step.payload))
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", step.action, err)
		}
		var res map[string]any
		if err := json.Unmarshal(resp.Result, &res); err != nil {
			t.Fatalf("%s result: %v", step.action, err)
		}
		if res["outcome"] != "applied" {
			t.Fatalf("%s outcome: %v", step.action, res)
		}
		found := false
		for _, h := range fake.calls() {
			if strings.HasPrefix(h, step.wantHit) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: tenant-local ArgoCD never saw %q; hits: %v", step.action, step.wantHit, fake.calls())
		}
	}
}

func TestFailsClosedWhenAgentDisconnected(t *testing.T) {
	c, _, gwSrv, cancel := startStack(t)
	_ = gwSrv
	cancel() // stop the agent loop; session still registered, so also deregister:
	// Simulate disconnect by invoking a cluster with no agent.
	auth := pluginsdk.AuthContext{PrincipalID: "dev-1", TenantID: "tenant-acme"}
	payload, _ := json.Marshal(map[string]any{
		"clusterId": "cluster-offline",
		"app":       map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
	})
	ctx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	_, err := c.Invoke(ctx, argocd.ActionSync, auth, payload)
	var pe *pluginsdk.Error
	if err == nil || !errors.As(err, &pe) || pe.Code != pluginsdk.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable when agent disconnected, got %v", err)
	}
	fmt.Println("fails closed OK:", err)
}
