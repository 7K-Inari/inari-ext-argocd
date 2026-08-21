import { useEffect, useState } from 'react';
import type { ClusterTabSlotProps } from '@7k-inari/ui-plugin-sdk';
import { listClusterInstances, type ResourceInstance } from './api';

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
  const [instances, setInstances] = useState<ResourceInstance[]>([]);
  const [error, setError] = useState<string>();

  useEffect(() => {
    let cancelled = false;
    listClusterInstances(cluster.id)
      .then((list) => !cancelled && setInstances(list))
      .catch((e: Error) => !cancelled && setError(e.message));
    return () => {
      cancelled = true;
    };
  }, [cluster.id]);

  return (
    <section>
      <h3>ArgoCD health — {cluster.name}</h3>
      {error && <p role="alert">Failed to load instances: {error}</p>}
      {!error && instances.length === 0 && <p>No Inari-managed instances on this cluster.</p>}
      <ul>
        {instances.map((i) => (
          <li key={i.id}>
            <strong>{i.name}</strong> {i.namespace ? `(${i.namespace}) ` : ''}
            <HealthBadge health={i.health} />
          </li>
        ))}
      </ul>
    </section>
  );
}
