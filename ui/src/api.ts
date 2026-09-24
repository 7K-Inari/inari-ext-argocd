// Control-plane API access for the extension's UI remote.
//
// The Inari host shell serves remotes same-origin, so calls go to relative
// paths with session credentials; the control plane authenticates, enforces
// extension RBAC (`extensions:invoke:inari-ext-argocd`), and proxies
// `/api/extensions/inari-ext-argocd/*` to the backend plugin (plan §5.8).
//
// The control plane authenticates with Bearer tokens (OIDC), not cookies:
// remotes must attach the shell's token. Components call configureAuth from
// an effect with the SDK's useAuth().getToken (the SDK is a host-provided
// singleton, so this is the shell's live session).

export const EXTENSION_NAME = 'inari-ext-argocd';

type TokenProvider = () => Promise<string | undefined> | string | undefined;

let tokenProvider: TokenProvider | null = null;

/** configureAuth installs the shell's token provider (call once per mounted
 * extension component; idempotent). */
export function configureAuth(getToken: TokenProvider): void {
  tokenProvider = getToken;
}

async function resolveToken(): Promise<string | undefined> {
  if (tokenProvider) return tokenProvider();
  // Fallback for action runners invoked outside any extension component:
  // read the shared SDK's module-level auth state.
  const { getAuthState } = await import('@7k-inari/ui-plugin-sdk');
  return getAuthState()?.getToken();
}

export type { ResourceInstance } from '@7k-inari/ui-plugin-sdk';
import type { ResourceInstance } from '@7k-inari/ui-plugin-sdk';

export interface ActionResult {
  commandId: string;
  outcome: 'applied' | 'accepted';
  message?: string;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const token = await resolveToken();
  const headers: Record<string, string> = { 'content-type': 'application/json' };
  if (token) headers['authorization'] = `Bearer ${token}`;
  const res = await fetch(path, {
    method,
    credentials: 'include',
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`${method} ${path} failed: ${res.status}`);
  }
  return (await res.json()) as T;
}

/** invokeAction calls one backend plugin action via the control plane proxy. */
export function invokeAction(action: string, payload: unknown): Promise<ActionResult> {
  return request<ActionResult>(
    'POST',
    `/api/extensions/${EXTENSION_NAME}/actions/${encodeURIComponent(action)}`,
    payload,
  );
}

/** listClusterInstances reads the host's resource inventory for a cluster. */
export async function listClusterInstances(
  org: string,
  clusterId: string,
): Promise<ResourceInstance[]> {
  // The server wraps lists: { instances: [...] }.
  const res = await request<{ instances: ResourceInstance[] | null }>(
    'GET',
    `/api/v1/tenants/${encodeURIComponent(org)}/instances?clusterId=${encodeURIComponent(clusterId)}`,
  );
  return res.instances ?? [];
}

/** argocdAppRef extracts the ArgoCD Application identity the orchestrator
 * records in instance status (status.argocd.{name,namespace,project}). */
export function argocdAppRef(instance: ResourceInstance): {
  name: string;
  namespace: string;
  project: string;
} {
  const argo = (instance.status?.argocd ?? {}) as Record<string, unknown>;
  return {
    name: (argo.name as string) ?? instance.name,
    namespace: (argo.namespace as string) ?? 'argocd',
    project: (argo.project as string) ?? 'inari',
  };
}
