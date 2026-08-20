import {
  createExtension,
  ClusterTabBlueprint,
  CatalogCardBlueprint,
  InstanceActionBlueprint,
} from '@inari/ui-plugin-sdk';
import { ArgoCDHealthTab } from './ClusterTab';
import { ArgoCDDeliveryBadge } from './CatalogCard';
import { runRefresh, runRollback, runSync } from './actions';

// The extension entry: manifest + slot contributions (blueprint contract,
// plan §5.8 / §8.4). Keep `version` in sync with ui/package.json
// (release-please node component).
export default createExtension({
  manifest: {
    name: 'inari-ext-argocd',
    version: '0.1.0', // x-release-please-version
    kind: 'ui',
    title: 'ArgoCD',
    description: 'ArgoCD health, status badges, and imperative GitOps actions (sync/refresh/rollback).',
  },
  slots: [
    ClusterTabBlueprint({
      name: 'argocd-health',
      title: 'ArgoCD',
      component: ArgoCDHealthTab,
    }),
    CatalogCardBlueprint({
      name: 'argocd-badge',
      component: ArgoCDDeliveryBadge,
    }),
    InstanceActionBlueprint({ name: 'argocd-sync', label: 'Sync (ArgoCD)', run: runSync }),
    InstanceActionBlueprint({ name: 'argocd-refresh', label: 'Refresh (ArgoCD)', run: runRefresh }),
    InstanceActionBlueprint({ name: 'argocd-rollback', label: 'Rollback (ArgoCD)', run: runRollback }),
  ],
});
