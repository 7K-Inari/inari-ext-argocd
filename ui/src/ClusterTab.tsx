import { useEffect, useState } from 'react';
import type { ClusterTabSlotProps } from '@7k-inari/ui-plugin-sdk';
import { useAuth, useTenant } from '@7k-inari/ui-plugin-sdk';
import { configureAuth, listClusterInstances, type ResourceInstance } from './api';

const HEALTH_COLORS: Record<string, string> = {
  Healthy: '#2da44e',
  Progressing: '#bf8700',
  Degraded: '#cf222e',
  Suspended: '#6e7781',
  Missing: '#cf222e',
  Unknown: '#6e7781',
};

export function HealthBadge({ health }: { health?: string }) {
  const h = health ?? 'Unknown';
  return (
    <span
      data-testid="health-badge"
      style={{
        background: HEALTH_COLORS[h] ?? HEALTH_COLORS.Unknown,
        color: '#fff',
        borderRadius: 10,
        padding: '2px 10px',
        fontSize: 12,
      }}
    >
      {h}
    </span>
  );
}

/** ArgoCDHealthTab is the ClusterTab contribution: ArgoCD health of every
 * resource instance on the cluster. */
export function ArgoCDHealthTab({ cluster }: ClusterTabSlotProps) {
  const { current } = useTenant();
  const auth = useAuth();
  const [instances, setInstances] = useState<ResourceInstance[]>([]);
  const [error, setError] = useState<string>();

  useEffect(() => configureAuth(() => auth.getToken()), [auth]);

  useEffect(() => {
    if (!current) return;
    let cancelled = false;
    listClusterInstances(current.orgId, cluster.id)
      .then((list) => !cancelled && setInstances(list))
      .catch((e: Error) => !cancelled && setError(e.message));
    return () => {
      cancelled = true;
    };
  }, [current, cluster.id]);

  return (
    <section>
      <h3>ArgoCD health — {cluster.name}</h3>
      {error && <p role="alert">Failed to load instances: {error}</p>}
      {!error && instances.length === 0 && <p>No Inari-managed instances on this cluster.</p>}
      <ul style={{ listStyle: 'none', padding: 0, margin: '8px 0' }}>
        {instances.map((i) => {
          // Server returns state/statusMessage as top-level fields.
          const top = i as unknown as Record<string, unknown>;
          const state = (top.state as string) ?? '';
          const message = (top.statusMessage as string) ?? '';
          return (
            <li key={i.id} style={{ display: 'flex', alignItems: 'baseline', gap: 8, padding: '4px 0' }}>
              <strong>{i.name || i.id}</strong>
              <span style={{ color: '#6e7781', fontSize: 12 }}>{i.catalogItemId.replace(/^curated:/, '')}</span>
              {i.namespace ? <span style={{ color: '#6e7781', fontSize: 12 }}>({i.namespace})</span> : null}
              <HealthBadge health={i.health} />
              {state && state !== 'running' ? (
                <span style={{ color: '#6e7781', fontSize: 12 }}>{state}</span>
              ) : null}
              {message ? (
                <span title={message} style={{ color: '#bf8700', fontSize: 12, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: 360 }}>
                  {message}
                </span>
              ) : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}
