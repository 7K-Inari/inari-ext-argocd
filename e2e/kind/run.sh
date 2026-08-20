#!/usr/bin/env bash
# Kind end-to-end: plugin (control-plane side) -> e2e Agent Gateway -> agent
# session -> tenant-local ArgoCD in kind, and back (plan §5.3).
#
# Prerequisites: docker, kind, kubectl, go.
set -euo pipefail
cd "$(dirname "$0")/../.."

CLUSTER="${KIND_CLUSTER:-inari-e2e}"
GW_ADDR="127.0.0.1:9090"
ARGOCD_PF_PORT=8080
ARGOCD_VERSION="${ARGOCD_VERSION:-stable}"

cleanup() {
  set +e
  [ -n "${GW_PID:-}" ] && kill "$GW_PID"
  [ -n "${PF_PID:-}" ] && kill "$PF_PID"
  if [ "${KEEP_CLUSTER:-}" != "1" ]; then kind delete cluster --name "$CLUSTER"; fi
}
trap cleanup EXIT

echo "==> kind cluster $CLUSTER"
kind create cluster --name "$CLUSTER" --wait 60s

echo "==> install ArgoCD ($ARGOCD_VERSION)"
kubectl create namespace argocd --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -n argocd -f "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml"
kubectl -n argocd rollout status deploy/argocd-server --timeout=300s

echo "==> Inari-managed AppProject + sample Application (guestbook)"
kubectl apply -f e2e/kind/manifests/project-inari.yaml
kubectl apply -f e2e/kind/manifests/app-guestbook.yaml

echo "==> port-forward argocd-server :$ARGOCD_PF_PORT"
kubectl -n argocd port-forward svc/argocd-server "$ARGOCD_PF_PORT":80 >/dev/null &
PF_PID=$!
for i in $(seq 1 30); do curl -sf "http://127.0.0.1:$ARGOCD_PF_PORT/healthz" >/dev/null && break; sleep 1; done

echo "==> build plugin + e2e gateway"
go build -o bin/inari-ext-argocd ./cmd/inari-ext-argocd
go build -o bin/inari-e2e-gateway ./cmd/inari-e2e-gateway
go build -o bin/e2e-driver ./e2e/kind/driver

echo "==> start e2e gateway (agent session -> ArgoCD)"
CLUSTER_ID=e2e-kind LISTEN_ADDR="$GW_ADDR" ARGOCD_BASE_URL="http://127.0.0.1:$ARGOCD_PF_PORT" \
  bin/inari-e2e-gateway &
GW_PID=$!
sleep 2

echo "==> run driver (control plane role)"
PLUGIN_BINARY=bin/inari-ext-argocd AGENT_GATEWAY_ADDR="$GW_ADDR" CLUSTER_ID=e2e-kind bin/e2e-driver

echo "==> E2E PASSED"
