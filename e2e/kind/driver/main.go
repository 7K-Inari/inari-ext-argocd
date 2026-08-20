// Command e2e-driver plays the control-plane role in the kind e2e: it
// launches the plugin as a supervised subprocess (inari-plugin-sdk/host, the
// same contract inari-server's Extension Host uses), invokes all four ArgoCD
// actions through the Agent Gateway, and asserts every round trip succeeds.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"github.com/7K-Inari/inari-plugin-sdk/host"
)

func main() {
	pluginBin := os.Getenv("PLUGIN_BINARY")
	gatewayAddr := os.Getenv("AGENT_GATEWAY_ADDR")
	clusterID := envOr("CLUSTER_ID", "e2e-kind")
	if pluginBin == "" || gatewayAddr == "" {
		log.Fatal("PLUGIN_BINARY and AGENT_GATEWAY_ADDR are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client, err := host.Launch(ctx, pluginBin, host.WithEnv(
		"INARI_AGENT_GATEWAY_ADDR="+gatewayAddr,
		"INARI_AGENT_GATEWAY_INSECURE=true",
		"INARI_MANAGED_PROJECTS=inari",
	))
	if err != nil {
		log.Fatalf("launch plugin: %v", err)
	}
	defer client.Close()

	info, err := client.Info(ctx)
	if err != nil {
		log.Fatalf("info: %v", err)
	}
	fmt.Printf("plugin %s@%s (contract %s) handshook OK\n", info.GetName(), info.GetVersion(), info.GetApiVersion())

	auth := pluginsdk.AuthContext{PrincipalID: "e2e-driver", TenantID: "tenant-e2e"}
	app := map[string]any{"name": envOr("E2E_APP_NAME", "guestbook"), "namespace": "argocd", "project": "inari"}

	type step struct {
		action  string
		payload map[string]any
	}
	steps := []step{
		{"argocd.refresh", map[string]any{"clusterId": clusterID, "app": app, "hard": true}},
		{"argocd.sync", map[string]any{"clusterId": clusterID, "app": app}},
		{"argocd.resource-action", map[string]any{
			"clusterId": clusterID, "app": app,
			"resource": map[string]any{"kind": "Deployment", "name": "guestbook-ui", "namespace": "default"},
			"action":   "restart",
		}},
		{"argocd.rollback", map[string]any{"clusterId": clusterID, "app": app, "revisionId": 1, "dryRun": true}},
	}
	for _, s := range steps {
		payload, _ := json.Marshal(s.payload)
		resp, err := client.Invoke(ctx, s.action, auth, payload)
		if err != nil {
			log.Fatalf("FAIL %s: %v", s.action, err)
		}
		fmt.Printf("PASS %s -> %s\n", s.action, string(resp.Result))
	}

	// Negative check: unmanaged project must be rejected.
	payload, _ := json.Marshal(map[string]any{
		"clusterId": clusterID,
		"app":       map[string]any{"name": "guestbook", "namespace": "argocd", "project": "default"},
	})
	if _, err := client.Invoke(ctx, "argocd.sync", auth, payload); err == nil {
		log.Fatal("FAIL: sync on unmanaged project 'default' was not rejected")
	}
	fmt.Println("PASS scoping: unmanaged project rejected")
	fmt.Println("E2E OK: all actions round-tripped control plane -> agent -> tenant-local ArgoCD")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
