import { dateLocale } from '@/shared/i18n';
import type { Locale } from '@/shared/lib/languageStore';
import type { BadgeVariant } from '@/shared/ui/Badge';

// The row classes every all-users table shares; each table adds its grid.
export const resourceRowClass = 'grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center';

// statusVariant is the badge for a volume, image, build or project status;
// containers use the container model's mapping.
export function statusVariant(status: string): BadgeVariant {
  if (['running', 'success', 'available', 'active'].includes(status)) return 'success';
  if (['pending', 'creating', 'starting', 'deploying', 'building'].includes(status)) return 'info';
  if (['stopping', 'deleting', 'canceling', 'canceled'].includes(status)) return 'warning';
  if (status === 'missing' || status === 'failed' || status.startsWith('failed') || status === 'error') return 'error';
  return 'default';
}

export function formatDateTime(value: number, locale: Locale) {
  if (!value) return '-';
  return new Date(value * 1000).toLocaleString(dateLocale(locale));
}

// ownerName names a resource's owner for the all-users tables.
export function ownerName(resource: { owner_username?: string; owner_id?: string }) {
  return resource.owner_username || resource.owner_id || 'unknown';
}
