# inari-ext-argocd

Reference + first-party Inari extension: ArgoCD actions (sync / refresh /
rollback / custom Lua resource actions) and ArgoCD status UI (cluster tab,
instance action buttons, catalog badges). **Third parties: this repo is the
reference implementation to copy** when writing your own Inari extension
(platform plan §5.8 dogfooding: it is built only on the public SDKs).

Part of the **Inari** multi-tenant Internal Developer Platform (GitHub org `7K-Inari`).
Canonical architecture: [inari-platform-plan.md](https://github.com/7K-Inari/inari-docs/blob/main/docs/architecture/inari-platform-plan.md) (§5.3 imperative ops, §5.8 extensibility, §6 artifacts, §8.4 UI slots).

## Architecture

```
Browser (Module Federation host shell)
  └─ ui/ remote "inari_ext_argocd"           ← ClusterTab, InstanceActions, CatalogCard
        │  POST /api/extensions/inari-ext-argocd/actions/<action>
        ▼
Control plane (inari-server)                 ← authenticates, enforces RBAC
  │    `extensions:invoke:inari-ext-argocd`, reverse-proxies to the plugin
  ▼
Backend plugin (this repo, Go)               ← inari-plugin-sdk contract
  │    validates + scopes (Inari-managed AppProjects only), binds the
  │    authenticated tenant from AuthContext — never from the payload
  ▼
Agent Gateway  ──agent stream──▶  inari-agent in the tenant cluster
                                     ▼
                            tenant-local ArgoCD API
```

Fail closed everywhere: no agent session / unreachable gateway / unmanaged
project → the action is rejected (`CodeUnavailable` / `CodeInvalidArgument`)
before any mutation.

## Repository layout

| Path | What |
|---|---|
| `cmd/inari-ext-argocd` | backend plugin entrypoint (env-configured) |
| `internal/extplugin` | plugin wiring: actions, JSON schemas, handlers, health |
| `internal/argocd` | payload types, validation, `agentv1.InvokeAction` construction |
| `internal/gateway` | Agent Gateway tunnel client (`/inari.extensions.v1.AgentGateway/InvokeAction`, protojson, `x-inari-tenant`/`x-inari-cluster` metadata) |
| `internal/agentstub` | reference executor: InvokeAction → tenant-local ArgoCD REST |
| `e2e/` | in-process round-trip tests + gateway harness |
| `e2e/kind` | full kind + ArgoCD e2e (`make e2e-kind`), driver, manifests |
| `ui/` | Module Federation remote (webpack), vitest tests |
| `extension.yaml` | extension manifest (kinds, actions, slots, permissions) |
| `Dockerfile` | backend image (distroless, nonroot) |

## Develop

```sh
make test          # go test ./...  (includes in-process e2e round trip)
make ui-test       # vitest
make ui-build      # typecheck + webpack → ui/dist/remoteEntry.js
make e2e-kind      # real round trip on kind (docker/kind/kubectl required)
```

Backend config (env): `INARI_AGENT_GATEWAY_ADDR` (required in prod),
`INARI_AGENT_GATEWAY_INSECURE`, `INARI_AGENT_GATEWAY_TLS_NAME`,
`INARI_MANAGED_PROJECTS` (default `inari`), `INARI_ACTION_TIMEOUT`.

UI local dev: `cd ui && npm install && npx inari-ui-ext dev ./src/index.tsx`
(SDK dev harness with a mock control plane).

## Actions

| Action | Payload | Tenant-local ArgoCD call |
|---|---|---|
| `argocd.sync` | clusterId, app{name,namespace,project}, prune, dryRun, strategy | `POST /api/v1/applications/<app>/sync` |
| `argocd.refresh` | clusterId, app, hard | `GET /api/v1/applications/<app>?refresh=normal\|hard` |
| `argocd.rollback` | clusterId, app, revisionId, prune, dryRun | `POST .../rollback` |
| `argocd.resource-action` | clusterId, app, resource{kind,name,...}, action (Lua name), params | `POST .../resource/actions?...` |

All actions require `app.project` ∈ `INARI_MANAGED_PROJECTS`. The agent-side
executor re-checks the same allowlist (defense in depth).

## Writing your own extension (walkthrough)

1. **Backend**: `pluginsdk.New(Info{Name, Version})` → `RegisterAction` with
   JSON schemas + handler → `p.Serve(ctx)` (see `cmd/inari-ext-argocd`).
   Get the caller's tenant via `pluginsdk.AuthContextFrom(ctx)`; return
   errors with `pluginsdk.Errorf(code, ...)`. Test in-process with
   `inari-plugin-sdk/testkit` (see `internal/extplugin/plugin_test.go`).
2. **UI**: `createExtension({manifest, slots})` with blueprint contributions
   (see `ui/src/index.tsx`); mark react/react-dom/zod/SDK as shared
   singletons in your webpack MF config (see `ui/webpack.config.mjs`);
   expose `./extension` as `remoteEntry.js`.
3. **Manifest**: copy `extension.yaml` (name, version, kinds, actions, slots,
   permissions) and register it with the control plane.
4. **Release**: push Conventional Commits; release-please opens one Release
   PR tracking both components (`.` = Go backend → tag `v*`, image to GHCR,
   cosign keyless; `ui/` = node → tag `ui-v*`, `remoteEntry.js` attached to
   the UI GitHub Release). See `.github/workflows/release*.yml`.

## SDK gaps (found while dogfooding)

These are blockers/limitations in the public SDKs that this repo works around
and that should be fixed upstream:

1. **No host service for imperative tenant ops** — the Go SDK gives plugins
   only `AuthContext`; there is no SDK-level way to reach the agent gateway.
   This extension dials a raw gRPC endpoint (`internal/gateway`). The SDK (or
   inari-api) should provide a typed `AgentGateway` host service.
2. **No `AgentGateway` proto in `inari-api`** — the tunnel contract
   (`/inari.extensions.v1.AgentGateway/InvokeAction`, protojson payload of
   `agentv1.InvokeAction`/`CommandAck`, tenant/cluster routing metadata) is
   defined in this repo and mirrored by `e2e/harness`. It belongs in
   inari-api as a versioned proto.
3. **No extension-invoke helper in the UI SDK** — remotes must hand-roll
   `fetch('/api/extensions/<name>/actions/<action>', {credentials:'include'})`
   (`ui/src/api.ts`). The SDK's `ApiClient` should gain `invokeExtension()`
   with host token injection; the exact proxy path convention needs
   confirmation from the M4-W2 extension host.
4. **`@inari/ui-plugin-sdk` is not published to npm**, and its `files`
   whitelist makes the npm git tarball unbuildable (no `tsup.config.ts`).
   `ui/scripts/build-sdk.mjs` clones + builds the SDK as a postinstall
   workaround. Publish the SDK (or add a `prepare` script) and delete the
   workaround.
5. **`inari-ui-ext` CLI has no `build` command** (only `dev`/`init`), so this
   repo ships its own webpack MF production config. A canonical
   `inari-ui-ext build` emitting `remoteEntry.js` would keep third parties
   from drifting.

## Versioning & release

Two independently versioned artifacts (plan §6): backend image
(`ghcr.io/7k-inari/inari-ext-argocd:v*`, cosign keyless-signed) and UI remote
(`remoteEntry.js` on `ui-v*` GitHub Releases), managed by release-please
manifest mode (`.release-please-manifest.json`, `release-please-config.json`).
