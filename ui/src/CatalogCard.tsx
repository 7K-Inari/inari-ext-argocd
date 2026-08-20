import type { CatalogCardSlotProps } from '@inari/ui-plugin-sdk';
import { HealthBadge } from './ClusterTab';

/** ArgoDCDeliveryBadge marks catalog items whose instances are delivered via
 * tenant-local ArgoCD (uiHints['inari.io/gitops'] === 'argocd'), and shows
 * live health when the catalog entry carries it in uiHints. */
export function ArgoCDDeliveryBadge({ catalogItem }: CatalogCardSlotProps) {
  const hints = catalogItem.uiHints ?? {};
  if (hints['inari.io/gitops'] !== 'argocd') {
    return null;
  }
  const health = hints['inari.io/health'] as string | undefined;
  return (
    <span title="Delivered via tenant-local ArgoCD">
      GitOps: ArgoCD {health ? <HealthBadge health={health} /> : null}
    </span>
  );
}
