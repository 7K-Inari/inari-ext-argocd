# inari-ext-argocd — Agent Guide

Reference + first-party Inari extension: ArgoCD actions (sync/refresh/rollback/custom Lua actions) and ArgoCD status cards/tabs for the UI (plan §6 #11).

Stack: Go (backend plugin) + TypeScript (UI remote)

## Key architecture constraints
- **Dogfooding**: built only on the public SDKs (`inari-plugin-sdk`, `inari-ui-plugin-sdk`) — if something is impossible for third parties, fix the SDK, not the extension (§5.8).
- Imperative ops (sync/refresh/rollback, Lua custom actions) go through the tenant-local ArgoCD API, tunneled over the agent stream, scoped to Inari-managed resources; fails closed when disconnected (§5.3).
- Versioned artifacts: container image + UI remote (§6).

## Conventions
- Conventional Commits; SemVer releases; container images/artifacts cosign-signed (once CI exists).
- **Release flow (release-please, PR-only mode):** pushes to `main` only open/update a Release PR (version bump + CHANGELOG) via `.github/workflows/release-please.yml` (`skip-github-release: true`). Manually merging the Release PR triggers `.github/workflows/release.yml` (push to `main`, detects the release merge by commit subject `chore: release …`) → creates tags + GitHub Releases → publish jobs: backend container image to GHCR (cosign keyless signing via OIDC) and UI `remoteEntry.js` uploaded to the UI GitHub Release. Publish jobs are guarded and no-op until the M4-W3 scaffold lands.
- **Versioning strategy: release-please manifest mode with two path components** (`.` → release-type `go` for the backend, `ui` → release-type `node` for the UI remote; see `release-please-config.json` / `.release-please-manifest.json`). Chosen over a single `go` release with `extra-files` generic updater for `ui/package.json` because the repo ships two independently versioned artifacts (plan §6: container image + UI remote) — each gets its own tag (`v*` backend, `ui-v*` UI) and release cadence, and the `node` type bumps `ui/package.json` natively instead of via brittle generic-updater regexes. One shared Release PR (`separate-pull-requests: false`).
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
