package argocd

import (
	"strings"
	"testing"
	"time"
)

var sc = Scoping{ManagedProjects: []string{"inari"}}

func app() AppRef { return AppRef{Name: "web-shop", Namespace: "argocd", Project: "inari"} }

func TestBuildSync(t *testing.T) {
	cmd, err := BuildSync("cmd-1", SyncInput{ClusterID: "c-1", App: app(), Prune: true, Strategy: "hook"}, sc, 0)
	if err != nil {
		t.Fatalf("BuildSync: %v", err)
	}
	if cmd.GetCommandId() != "cmd-1" || cmd.GetAction() != "sync" {
		t.Fatalf("unexpected command: %v", cmd)
	}
	if cmd.GetTimeout().AsDuration() != DefaultTimeout {
		t.Fatalf("default timeout not applied: %v", cmd.GetTimeout())
	}
	p := cmd.GetParameters().AsMap()
	appMap := p["app"].(map[string]any)
	if appMap["name"] != "web-shop" || appMap["project"] != "inari" {
		t.Fatalf("app params: %v", appMap)
	}
	if p["prune"] != true || p["strategy"] != "hook" {
		t.Fatalf("params: %v", p)
	}
}

func TestBuildRejectsUnmanagedProject(t *testing.T) {
	bad := app()
	bad.Project = "default"
	for name, build := range map[string]func() error{
		"sync":    func() error { _, e := BuildSync("c", SyncInput{ClusterID: "c-1", App: bad}, sc, 0); return e },
		"refresh": func() error { _, e := BuildRefresh("c", RefreshInput{ClusterID: "c-1", App: bad}, sc, 0); return e },
		"rollback": func() error {
			_, e := BuildRollback("c", RollbackInput{ClusterID: "c-1", App: bad, RevisionID: 3}, sc, 0)
			return e
		},
		"resourceAction": func() error {
			_, e := BuildResourceAction("c", ResourceActionInput{ClusterID: "c-1", App: bad, Resource: ResourceRef{Kind: "Deployment", Name: "x"}, Action: "restart"}, sc, 0)
			return e
		},
	} {
		if err := build(); err == nil || !strings.Contains(err.Error(), "not managed by Inari") {
			t.Fatalf("%s: expected scoping rejection, got %v", name, err)
		}
	}
}

func TestBuildValidation(t *testing.T) {
	cases := map[string]func() error{
		"missing cluster": func() error { _, e := BuildSync("c", SyncInput{App: app()}, sc, 0); return e },
		"bad app name": func() error {
			_, e := BuildSync("c", SyncInput{ClusterID: "c", App: AppRef{Name: "Bad_Name", Namespace: "argocd", Project: "inari"}}, sc, 0)
			return e
		},
		"missing project": func() error {
			_, e := BuildSync("c", SyncInput{ClusterID: "c", App: AppRef{Name: "a", Namespace: "argocd"}}, sc, 0)
			return e
		},
		"bad strategy": func() error {
			_, e := BuildSync("c", SyncInput{ClusterID: "c", App: app(), Strategy: "wipe"}, sc, 0)
			return e
		},
		"bad revision": func() error { _, e := BuildRollback("c", RollbackInput{ClusterID: "c", App: app()}, sc, 0); return e },
		"bad lua action": func() error {
			_, e := BuildResourceAction("c", ResourceActionInput{ClusterID: "c", App: app(), Resource: ResourceRef{Kind: "Deployment", Name: "x"}, Action: "Delete();"}, sc, 0)
			return e
		},
		"missing resource": func() error {
			_, e := BuildResourceAction("c", ResourceActionInput{ClusterID: "c", App: app(), Action: "restart"}, sc, 0)
			return e
		},
	}
	for name, build := range cases {
		if err := build(); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestBuildRollbackAndResourceAction(t *testing.T) {
	rb, err := BuildRollback("cmd-2", RollbackInput{ClusterID: "c-1", App: app(), RevisionID: 7, DryRun: true}, sc, 5*time.Second)
	if err != nil {
		t.Fatalf("BuildRollback: %v", err)
	}
	p := rb.GetParameters().AsMap()
	if p["id"].(float64) != 7 || p["dryRun"] != true {
		t.Fatalf("rollback params: %v", p)
	}
	if rb.GetTimeout().AsDuration() != 5*time.Second {
		t.Fatalf("timeout: %v", rb.GetTimeout())
	}

	ra, err := BuildResourceAction("cmd-3", ResourceActionInput{
		ClusterID: "c-1",
		App:       app(),
		Resource:  ResourceRef{Group: "apps", Kind: "Deployment", Name: "web", Namespace: "shop"},
		Action:    "restart",
		Params:    map[string]any{"wave": "1"},
	}, sc, 0)
	if err != nil {
		t.Fatalf("BuildResourceAction: %v", err)
	}
	p = ra.GetParameters().AsMap()
	res := p["resource"].(map[string]any)
	if res["kind"] != "Deployment" || res["group"] != "apps" || p["resourceAction"] != "restart" {
		t.Fatalf("resource action params: %v", p)
	}
}
