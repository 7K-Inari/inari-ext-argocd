import type { ResourceInstance } from '@inari/ui-plugin-sdk';
import { argocdAppRef, invokeAction } from './api';

// Instance action runners. The control plane proxies these calls to the
// backend plugin; the plugin tunnels them to the tenant-local ArgoCD API
// (plan §5.3). Payload shape matches the backend's JSON schemas.

function basePayload(instance: ResourceInstance) {
  return { clusterId: instance.clusterId, app: argocdAppRef(instance) };
}

export function runSync(instance: ResourceInstance): Promise<void> {
  return invokeAction('argocd.sync', basePayload(instance)).then(() => undefined);
}

export function runRefresh(instance: ResourceInstance): Promise<void> {
  return invokeAction('argocd.refresh', { ...basePayload(instance), hard: false }).then(() => undefined);
}

export function runRollback(instance: ResourceInstance): Promise<void> {
  const argo = (instance.status?.argocd ?? {}) as Record<string, unknown>;
  const revisionId = (argo.lastRevisionId as number) ?? 0;
  if (!revisionId) {
    return Promise.reject(
      new Error(`instance ${instance.name} has no recorded ArgoCD revision to roll back to`),
    );
  }
  return invokeAction('argocd.rollback', { ...basePayload(instance), revisionId }).then(() => undefined);
}
