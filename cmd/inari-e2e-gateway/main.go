// Command inari-e2e-gateway is the colocated e2e counterpart of the control
// plane's Agent Gateway + inari-agent pair, for the kind e2e (e2e/kind). It
// serves the tunnel contract (internal/gateway) on a TCP port and runs one
// agent session that executes commands against a reachable ArgoCD API
// (typically a kubectl port-forward into the kind cluster).
//
//	CLUSTER_ID             cluster identity to register (default "e2e-kind")
//	LISTEN_ADDR            gateway listen address (default "127.0.0.1:9090")
//	ARGOCD_BASE_URL        tenant-local ArgoCD API (default "http://127.0.0.1:8080")
//	ARGOCD_TOKEN           optional bearer token
//	INARI_MANAGED_PROJECTS comma list (default "inari")
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"google.golang.org/grpc"

	"github.com/7K-Inari/inari-ext-argocd/e2e/harness"
	"github.com/7K-Inari/inari-ext-argocd/internal/agentstub"
)

func main() {
	listen := envOr("LISTEN_ADDR", "127.0.0.1:9090")
	clusterID := envOr("CLUSTER_ID", "e2e-kind")

	lis, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("listen %s: %v", listen, err)
	}
	srv := harness.NewServer()
	grpcSrv := grpc.NewServer()
	desc := srv.ServiceDesc()
	grpcSrv.RegisterService(&desc, srv)

	sess, disconnect := srv.RegisterAgent(clusterID)
	defer disconnect()
	argo := &agentstub.ArgoCDClient{
		BaseURL:         envOr("ARGOCD_BASE_URL", "http://127.0.0.1:8080"),
		BearerToken:     os.Getenv("ARGOCD_TOKEN"),
		ManagedProjects: projects(),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go harness.RunAgent(ctx, sess, argo.Execute)
	go func() {
		<-ctx.Done()
		grpcSrv.GracefulStop()
	}()

	log.Printf("e2e gateway on %s, agent session for cluster %q against %s (managed projects: %v)",
		listen, clusterID, argo.BaseURL, argo.ManagedProjects)
	if err := grpcSrv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func projects() []string {
	return strings.Split(envOr("INARI_MANAGED_PROJECTS", "inari"), ",")
}
