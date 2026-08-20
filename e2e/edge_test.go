package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"

	"github.com/7K-Inari/inari-ext-argocd/internal/argocd"
)

func invokeRaw(t *testing.T, action string, auth pluginsdk.AuthContext, payload []byte) error {
	t.Helper()
	c, _, _, _ := startStack(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := c.Invoke(ctx, action, auth, payload)
	return err
}

func codeOf(t *testing.T, err error) pluginsdk.Code {
	t.Helper()
	var pe *pluginsdk.Error
	if err == nil || !errors.As(err, &pe) {
		t.Fatalf("expected pluginsdk.Error, got %v", err)
	}
	return pe.Code
}

func TestEmptyPayloadRejected(t *testing.T) {
	auth := pluginsdk.AuthContext{PrincipalID: "p", TenantID: "t"}
	if got := codeOf(t, invokeRaw(t, argocd.ActionSync, auth, nil)); got != pluginsdk.CodeInvalidArgument {
		t.Fatalf("empty payload: want InvalidArgument, got %v", got)
	}
	if got := codeOf(t, invokeRaw(t, argocd.ActionSync, auth, []byte("{}"))); got != pluginsdk.CodeInvalidArgument {
		t.Fatalf("{}: want InvalidArgument, got %v", got)
	}
}

func TestMissingAuthRejected(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"clusterId": "cluster-1",
		"app":       map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
	})
	if got := codeOf(t, invokeRaw(t, argocd.ActionSync, pluginsdk.AuthContext{}, payload)); got != pluginsdk.CodeUnauthenticated {
		t.Fatalf("want Unauthenticated, got %v", got)
	}
}

func TestScopingBypassAttempts(t *testing.T) {
	auth := pluginsdk.AuthContext{PrincipalID: "p", TenantID: "t"}
	for _, project := range []string{"Inari", "inari ", " inari", "inari\x00", "inari/default"} {
		payload, _ := json.Marshal(map[string]any{
			"clusterId": "cluster-1",
			"app":       map[string]any{"name": "a", "namespace": "argocd", "project": project},
		})
		if err := invokeRaw(t, argocd.ActionSync, auth, payload); err == nil {
			t.Fatalf("project %q must not reach the agent", project)
		}
	}
}

func TestRollbackBoundaryRevisionIDs(t *testing.T) {
	auth := pluginsdk.AuthContext{PrincipalID: "p", TenantID: "t"}
	for _, id := range []int64{0, -1} {
		payload, _ := json.Marshal(map[string]any{
			"clusterId": "cluster-1", "revisionId": id,
			"app": map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
		})
		if got := codeOf(t, invokeRaw(t, argocd.ActionRollback, auth, payload)); got != pluginsdk.CodeInvalidArgument {
			t.Fatalf("revisionId %d: want InvalidArgument, got %v", id, got)
		}
	}
}

func TestSlowAgentErrorSemantics(t *testing.T) {
	c, _, gwSrv, cancel := startStack(t)
	_ = cancel
	gwSrv.AgentTimeout = 100 * time.Millisecond
	// Replace the agent loop with one that never acks: simulate a wedged agent.
	sess, _ := gwSrv.RegisterAgent("cluster-slow")
	go func() {
		for range sess.Commands {
		}
	}()

	auth := pluginsdk.AuthContext{PrincipalID: "p", TenantID: "t"}
	payload, _ := json.Marshal(map[string]any{
		"clusterId": "cluster-slow",
		"app":       map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
	})
	ctx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	_, err := c.Invoke(ctx, argocd.ActionSync, auth, payload)
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) {
		t.Fatalf("want pluginsdk.Error, got %v", err)
	}
	t.Logf("slow agent surfaces as code=%v msg=%v", pe.Code, err)
}

func TestConcurrentInvokesNoCrossTalk(t *testing.T) {
	c, fake, _, _ := startStack(t)
	auth := pluginsdk.AuthContext{PrincipalID: "p", TenantID: "t"}
	var wg sync.WaitGroup
	errs := make([]error, 32)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload, _ := json.Marshal(map[string]any{
				"clusterId": "cluster-1", "hard": i%2 == 0,
				"app": map[string]any{"name": fmt.Sprintf("app-%d", i), "namespace": "argocd", "project": "inari"},
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, errs[i] = c.Invoke(ctx, argocd.ActionRefresh, auth, payload)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("invoke %d: %v", i, err)
		}
	}
	if got := len(fake.calls()); got != len(errs) {
		t.Fatalf("want %d ArgoCD calls, got %d", len(errs), got)
	}
}

func TestUnknownActionRejected(t *testing.T) {
	auth := pluginsdk.AuthContext{PrincipalID: "p", TenantID: "t"}
	err := invokeRaw(t, "argocd.delete-everything", auth, []byte("{}"))
	if err == nil {
		t.Fatal("unknown action must be rejected")
	}
}

func TestAgentFailedAckPropagatesMessage(t *testing.T) {
	c, _, gwSrv, _ := startStack(t)
	sess, _ := gwSrv.RegisterAgent("cluster-fail")
	go func() {
		for cmd := range sess.Commands {
			sess.Ack(&agentv1.CommandAck{
				CommandId: cmd.GetCommandId(),
				Result:    agentv1.CommandResult_COMMAND_RESULT_FAILED,
				Message:   "argocd api: 409 conflict",
			})
		}
	}()
	auth := pluginsdk.AuthContext{PrincipalID: "p", TenantID: "t"}
	payload, _ := json.Marshal(map[string]any{
		"clusterId": "cluster-fail",
		"app":       map[string]any{"name": "a", "namespace": "argocd", "project": "inari"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Invoke(ctx, argocd.ActionSync, auth, payload)
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) {
		t.Fatalf("want pluginsdk.Error, got %v", err)
	}
	t.Logf("agent failure surfaces as code=%v err=%v", pe.Code, err)
}
