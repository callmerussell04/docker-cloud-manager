import type { TFunction } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';

export const ranges = [
  { label: '24h', seconds: 24 * 60 * 60 },
  { label: '7d', seconds: 7 * 24 * 60 * 60 },
  { label: '30d', seconds: 30 * 24 * 60 * 60 },
  { label: '90d', seconds: 90 * 24 * 60 * 60 },
];

export const auditActions = [
  'container.create',
  'container.start',
  'container.stop',
  'container.delete',
  'container.expose',
  'volume.create',
  'volume.delete',
  'image.delete',
  'build.create_archive',
  'build.create_git',
  'build.cancel',
  'build.delete',
  'compose.upload_deploy',
  'compose.git_deploy',
  'project.start',
  'project.stop',
  'project.cancel',
  'project.delete',
  'auth.register',
  'auth.login',
  'auth.oidc_callback',
  'user.admin_create',
  'user.admin_update',
  'user.admin_deactivate',
  'user.admin_reactivate',
  'telemetry.logs_ticket',
  'telemetry.terminal_ticket',
];

export const auditResourceTypes = ['container', 'volume', 'image', 'build', 'project', 'user', 'auth', 'telemetry', 'system'];
export const userPageSize = 10;
export const auditPageSize = 20;

// rangeParams is the report window: `seconds` back from `to` (Unix seconds).
export function rangeParams(seconds: number, to: number) {
  return { from: to - seconds, to };
}

export function numberValue(value: number) {
  return new Intl.NumberFormat().format(value);
}

export function percentValue(value: number) {
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value)}%`;
}

export function chartSeriesLabel(t: TFunction, name: string) {
  switch (name) {
    case 'actualMemory':
      return t('admin.reports.chart.actualMemory');
    case 'reservedMemory':
      return t('admin.reports.chart.reservedMemory');
    case 'disk':
      return t('admin.reports.chart.disk');
    case 'cpu':
      return t('admin.reports.chart.cpu');
    default:
      return name;
  }
}

export function chartTooltipValue(value: unknown, name: unknown, item: { dataKey?: unknown }, t: TFunction) {
  const dataKey = String(item.dataKey ?? name);
  const numericValue = Number(value);
  return [
    dataKey === 'cpu' ? percentValue(numericValue) : formatBytes(numericValue),
    chartSeriesLabel(t, dataKey),
  ];
}
