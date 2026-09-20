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
			Addr:          addr,
			Insecure:      os.Getenv("INARI_AGENT_GATEWAY_INSECURE") == "true",
			TLSServerName: os.Getenv("INARI_AGENT_GATEWAY_TLS_NAME"),
			Token:         os.Getenv("INARI_EXTENSION_GATEWAY_TOKEN"),
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
