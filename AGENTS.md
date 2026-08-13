# inari-ext-argocd — Agent Guide

Reference + first-party Inari extension: ArgoCD actions (sync/refresh/rollback/custom Lua actions) and ArgoCD status cards/tabs for the UI (plan §6 #11).

Stack: Go (backend plugin) + TypeScript (UI remote)

## Key architecture constraints
- **Dogfooding**: built only on the public SDKs (`inari-plugin-sdk`, `inari-ui-plugin-sdk`) — if something is impossible for third parties, fix the SDK, not the extension (§5.8).
- Imperative ops (sync/refresh/rollback, Lua custom actions) go through the tenant-local ArgoCD API, tunneled over the agent stream, scoped to Inari-managed resources; fails closed when disconnected (§5.3).
- Versioned artifacts: container image + UI remote (§6).

## Conventions
- Conventional Commits; SemVer releases; container images/artifacts cosign-signed (once CI exists).
- Write tests for new behavior; keep changes minimal and focused.
- Canonical architecture & development plan: https://github.com/7K-Inari/inari-docs/blob/main/docs/architecture/inari-platform-plan.md (section references below point into it).

## Platform design principles (apply everywhere)
1. Tenant-aware to the core — every object carries a tenant ID; every API decision is tenant-scoped.
2. Zero tenant credentials on the hub — no tenant kubeconfigs or cloud keys in the control plane.
3. Pull, never push — agents dial out; the control plane never initiates connections into tenant networks.
4. Desired state, eventually reconciled — GitOps/CR-based mutations, not imperative RPCs.
5. The catalog is a projection of reality — capabilities are discovered, not declared.
6. Small kernel, everything else extension.
7. Modular monolith first — strict internal module boundaries.
