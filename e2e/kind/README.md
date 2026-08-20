# Kind end-to-end

Full imperative-op round trip (plan §5.3) on a real cluster:

```
e2e-driver (control-plane role, SDK host launcher)
  → plugin subprocess (inari-ext-argocd)
  → inari-e2e-gateway (Agent Gateway + agent session, colocated)
  → tenant-local ArgoCD API in kind (kubectl port-forward)
  → CommandAck all the way back
```

## Run

```sh
make e2e-kind          # creates kind cluster "inari-e2e", installs ArgoCD, runs all checks
KEEP_CLUSTER=1 make e2e-kind   # keep the cluster afterwards for inspection
```

Requires `docker`, `kind`, `kubectl`, `go`, and network access to fetch the
ArgoCD install manifest and the sample guestbook repo.

## What it verifies

1. Plugin handshake + capability discovery via the SDK host contract.
2. `argocd.refresh`, `argocd.sync`, `argocd.resource-action` (restart the
   guestbook Deployment), `argocd.rollback` (dry-run) round trips.
3. Scoping: an action against an unmanaged AppProject (`default`) is rejected
   before reaching the agent.

## Components

| Piece | Stands in for | Source |
|---|---|---|
| `e2e-driver` | inari-server Extension Host | `e2e/kind/driver` |
| `inari-e2e-gateway` | control-plane Agent Gateway + agent stream bridge | `cmd/inari-e2e-gateway`, `e2e/harness` |
| agent session loop | `inari-agent` imperative-op handler | `internal/agentstub` |

The gateway ↔ plugin contract (`/inari.extensions.v1.AgentGateway/InvokeAction`)
is the in-repo reference for the contract that should move into `inari-api`
(see README "SDK gaps").
