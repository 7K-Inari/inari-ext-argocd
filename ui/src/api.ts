// Control-plane API access for the extension's UI remote.
//
// The Inari host shell serves remotes same-origin, so calls go to relative
// paths with session credentials; the control plane authenticates, enforces
// extension RBAC (`extensions:invoke:inari-ext-argocd`), and proxies
// `/api/extensions/inari-ext-argocd/*` to the backend plugin (plan §5.8).
//
// NOTE (SDK gap, see README): the UI SDK's ApiClient does not yet expose an
// extension-invoke helper or token injection for remotes, so we use plain
// fetch with credentials:"include".

export const EXTENSION_NAME = 'inari-ext-argocd';

export type { ResourceInstance } from '@7k-inari/ui-plugin-sdk';
import type { ResourceInstance } from '@7k-inari/ui-plugin-sdk';

export interface ActionResult {
  commandId: string;
  outcome: 'applied' | 'accepted';
  message?: string;
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'include',
    headers: { 'content-type': 'application/json' },
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
export function listClusterInstances(org: string, clusterId: string): Promise<ResourceInstance[]> {
  return request<ResourceInstance[]>(
    'GET',
    `/api/v1/tenants/${encodeURIComponent(org)}/instances?clusterId=${encodeURIComponent(clusterId)}`,
  );
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
