import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { createElement } from 'react';

import extension from '../src/index';
import { invokeAction, argocdAppRef } from '../src/api';
import { runSync, runRefresh, runRollback } from '../src/actions';
import { ArgoCDHealthTab, HealthBadge } from '../src/ClusterTab';
import { TenantProvider, type TenantState } from '@7k-inari/ui-plugin-sdk';

const testTenant: TenantState = {
  current: { orgId: 'acme', orgName: 'Acme' },
  available: [{ orgId: 'acme', orgName: 'Acme' }],
  switchTenant: () => {},
  onTenantChange: () => () => {},
};
const withTenant = (el: React.ReactElement) => createElement(TenantProvider, { value: testTenant }, el);
import { ArgoCDDeliveryBadge } from '../src/CatalogCard';

const instance = {
  id: 'ri-1',
  catalogItemId: 'ci-1',
  clusterId: 'cluster-1',
  name: 'web-shop',
  namespace: 'shop',
  health: 'Healthy',
  status: { argocd: { name: 'web-shop', namespace: 'argocd', project: 'inari', lastRevisionId: 4 } },
};

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('extension manifest (blueprint contract)', () => {
  it('registers name, kind and all five slots', () => {
    expect(extension.manifest.name).toBe('inari-ext-argocd');
    expect(extension.manifest.kind).toBe('ui');
    const slots = extension.manifest.slots.map((s) => `${s.kind}:${s.name}`);
    expect(slots).toEqual([
      'cluster-tab:argocd-health',
      'catalog-card:argocd-badge',
      'instance-action:argocd-sync',
      'instance-action:argocd-refresh',
      'instance-action:argocd-rollback',
    ]);
  });
});

describe('invokeAction', () => {
  it('POSTs to the control-plane extension proxy', async () => {
    const spy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response('{"commandId":"c1","outcome":"applied"}', { status: 200 }));
    const result = await invokeAction('argocd.sync', { clusterId: 'cluster-1' });
    expect(result.outcome).toBe('applied');
    const [url, init] = spy.mock.calls[0];
    expect(url).toBe('/api/extensions/inari-ext-argocd/actions/argocd.sync');
    expect((init as RequestInit).method).toBe('POST');
    expect(JSON.parse((init as RequestInit).body as string)).toEqual({ clusterId: 'cluster-1' });
  });

  it('throws on non-2xx', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('nope', { status: 403 }));
    await expect(invokeAction('argocd.sync', {})).rejects.toThrow('403');
  });
});

describe('instance action runners', () => {
  it('sync sends clusterId and app ref from instance status', async () => {
    const spy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response('{"commandId":"c","outcome":"applied"}', { status: 200 }));
    await runSync(instance);
    expect(JSON.parse((spy.mock.calls[0][1] as RequestInit).body as string)).toEqual({
      clusterId: 'cluster-1',
      app: { name: 'web-shop', namespace: 'argocd', project: 'inari' },
    });
  });

  it('refresh defaults to non-hard', async () => {
    const spy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response('{"commandId":"c","outcome":"applied"}', { status: 200 }));
    await runRefresh(instance);
    expect(JSON.parse((spy.mock.calls[0][1] as RequestInit).body as string).hard).toBe(false);
  });

  it('rollback requires a recorded revision', async () => {
    await expect(runRollback({ ...instance, status: {} })).rejects.toThrow(/no recorded ArgoCD revision/);
    const spy = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValue(new Response('{"commandId":"c","outcome":"applied"}', { status: 200 }));
    await runRollback(instance);
    expect(JSON.parse((spy.mock.calls[0][1] as RequestInit).body as string).revisionId).toBe(4);
  });
});

describe('argocdAppRef', () => {
  it('falls back to instance name and defaults', () => {
    expect(argocdAppRef({ ...instance, status: undefined })).toEqual({
      name: 'web-shop',
      namespace: 'argocd',
      project: 'inari',
    });
  });
});

describe('components', () => {
  it('HealthBadge renders health', () => {
    expect(renderToStaticMarkup(createElement(HealthBadge, { health: 'Degraded' }))).toContain('Degraded');
  });

  it('ClusterTab lists instances with health badges', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify([instance]), { status: 200 }),
    );
    const { createRoot } = await import('react-dom/client');
    const { act } = await import('react');
    const el = document.createElement('div');
    document.body.appendChild(el);
    const root = createRoot(el);
    await act(async () => {
      root.render(withTenant(createElement(ArgoCDHealthTab, { cluster: { id: 'cluster-1', name: 'prod-1', tenantId: 't', state: 'Active' } })));
    });
    expect(el.innerHTML).toContain('ArgoCD health — prod-1');
    expect(el.innerHTML).toContain('web-shop');
    expect(el.innerHTML).toContain('Healthy');
    root.unmount();
  });

  it('CatalogCard renders badge only for argocd-delivered items', () => {
    const off = renderToStaticMarkup(
      createElement(ArgoCDDeliveryBadge, { catalogItem: { id: 'c', name: 'x', source: 'curated', version: '1' } }),
    );
    expect(off).toBe('');
    const on = renderToStaticMarkup(
      createElement(ArgoCDDeliveryBadge, {
        catalogItem: {
          id: 'c',
          name: 'x',
          source: 'curated',
          version: '1',
          uiHints: { 'inari.io/gitops': 'argocd', 'inari.io/health': 'Healthy' },
        },
      }),
    );
    expect(on).toContain('GitOps: ArgoCD');
    expect(on).toContain('Healthy');
  });
});
