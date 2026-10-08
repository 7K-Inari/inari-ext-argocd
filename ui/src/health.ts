// Maps ArgoCD health states to 7k-design-system badge variants. The host
// loads the design-system stylesheet, so class names (never inline styles or
// raw hexes) are all an extension needs.
export const HEALTH_TO_BADGE: Record<string, string> = {
  Healthy: 'badge badge-success',
  Progressing: 'badge badge-warning',
  Degraded: 'badge badge-danger',
  Missing: 'badge badge-danger',
  Suspended: 'badge badge-neutral',
  Unknown: 'badge badge-neutral',
};

export function healthBadgeClass(health?: string): string {
  return HEALTH_TO_BADGE[health ?? 'Unknown'] ?? HEALTH_TO_BADGE.Unknown;
}
