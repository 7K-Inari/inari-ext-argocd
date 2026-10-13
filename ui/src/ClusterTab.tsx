import { useEffect, useState } from 'react';
import type { ClusterTabSlotProps } from '@7k-inari/ui-plugin-sdk';
import { useAuth, useTenant } from '@7k-inari/ui-plugin-sdk';
import { configureAuth, listClusterInstances, type ResourceInstance } from './api';
import { healthBadgeClass } from './health';

export function HealthBadge({ health }: { health?: string }) {
  const h = health ?? 'Unknown';
  return (
    <span data-testid="health-badge" className={healthBadgeClass(h)}>
      {h}
    </span>
  );
}

/** ArgoCDHealthTab is the ClusterTab contribution: ArgoCD health of every
 * resource instance on the cluster. Rendered with 7k-design-system classes —
 * the host console loads the stylesheet and tokens. */
export function ArgoCDHealthTab({ cluster }: ClusterTabSlotProps) {
  const { current } = useTenant();
  const auth = useAuth();
  const [instances, setInstances] = useState<ResourceInstance[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>();

  useEffect(() => configureAuth(() => auth.getToken()), [auth]);

  useEffect(() => {
    if (!current) {
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    listClusterInstances(current.orgId, cluster.id)
      .then((list) => !cancelled && setInstances(list))
      .catch((e: Error) => !cancelled && setError(e.message))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, [current, cluster.id]);

  return (
    <section className="card">
      <div className="card-header">
        <h3 className="mono-label">ArgoCD health — {cluster.name}</h3>
      </div>
      <div className="card-body">
        {error && (
          <p role="alert" className="alert alert-danger">
            Failed to load instances: {error}
          </p>
        )}
        {!error && loading && (
          <span className="skeleton" aria-busy="true" style={{ display: 'block', height: 48 }} />
        )}
        {!error && !loading && instances.length === 0 && (
          <div className="empty">
            <span className="empty-title">No instances</span>
            <span className="empty-text">No Inari-managed instances on this cluster.</span>
          </div>
        )}
        <ul className="list">
          {instances.map((i) => {
            // Server returns state/statusMessage as top-level fields.
            const top = i as unknown as Record<string, unknown>;
            const state = (top.state as string) ?? '';
            const message = (top.statusMessage as string) ?? '';
            return (
              <li key={i.id} className="list-item argocd-instance-row">
                <strong>{i.name || i.id}</strong>
                <span className="tag">{i.catalogItemId.replace(/^curated:/, '')}</span>
                {i.namespace ? <span className="tag">({i.namespace})</span> : null}
                <HealthBadge health={i.health} />
                {state && state !== 'running' ? <span className="tag">{state}</span> : null}
                {message ? (
                  <span className="badge badge-warning" title={message}>
                    {message}
                  </span>
                ) : null}
              </li>
            );
          })}
        </ul>
      </div>
    </section>
  );
}
