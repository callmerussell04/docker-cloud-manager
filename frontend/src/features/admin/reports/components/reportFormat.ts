import { dateLocale, labelFor, type TFunction, type TranslationKey } from '@/shared/i18n';
import type { Locale } from '@/shared/lib/languageStore';
import type { ReportRange } from '../model';

// auditActionLabels names every action Gateway and Core record, in filter order.
const auditActionLabels = {
  'container.create': 'admin.reports.auditAction.container.create',
  'container.start': 'admin.reports.auditAction.container.start',
  'container.stop': 'admin.reports.auditAction.container.stop',
  'container.delete': 'admin.reports.auditAction.container.delete',
  'container.expose': 'admin.reports.auditAction.container.expose',
  'volume.create': 'admin.reports.auditAction.volume.create',
  'volume.delete': 'admin.reports.auditAction.volume.delete',
  'image.delete': 'admin.reports.auditAction.image.delete',
  'build.create_archive': 'admin.reports.auditAction.build.create_archive',
  'build.create_git': 'admin.reports.auditAction.build.create_git',
  'build.cancel': 'admin.reports.auditAction.build.cancel',
  'build.delete': 'admin.reports.auditAction.build.delete',
  'compose.upload_deploy': 'admin.reports.auditAction.compose.upload_deploy',
  'compose.git_deploy': 'admin.reports.auditAction.compose.git_deploy',
  'project.start': 'admin.reports.auditAction.project.start',
  'project.stop': 'admin.reports.auditAction.project.stop',
  'project.cancel': 'admin.reports.auditAction.project.cancel',
  'project.delete': 'admin.reports.auditAction.project.delete',
  'auth.register': 'admin.reports.auditAction.auth.register',
  'auth.login': 'admin.reports.auditAction.auth.login',
  'auth.oidc_callback': 'admin.reports.auditAction.auth.oidc_callback',
  'auth.provider_logout': 'admin.reports.auditAction.auth.provider_logout',
  'user.admin_create': 'admin.reports.auditAction.user.admin_create',
  'user.admin_update': 'admin.reports.auditAction.user.admin_update',
  'user.admin_deactivate': 'admin.reports.auditAction.user.admin_deactivate',
  'user.admin_reactivate': 'admin.reports.auditAction.user.admin_reactivate',
  'user.quota_set': 'admin.reports.auditAction.user.quota_set',
  'user.quota_reset': 'admin.reports.auditAction.user.quota_reset',
  'telemetry.logs_ticket': 'admin.reports.auditAction.telemetry.logs_ticket',
  'telemetry.terminal_ticket': 'admin.reports.auditAction.telemetry.terminal_ticket',
} as const satisfies Record<string, TranslationKey>;

export const auditActions = Object.keys(auditActionLabels);

// auditActionLabel shows an action by its localized name, or as recorded when
// the page does not know it.
export function auditActionLabel(t: TFunction, action: string) {
  return labelFor(t, auditActionLabels, action);
}

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

// chartTime writes a time on a chart's axis: the hour over 24 h, the day
// over longer ranges.
export function chartTime(locale: Locale, range: ReportRange) {
  const format = new Intl.DateTimeFormat(dateLocale(locale), range === '24h' ? { hour: '2-digit', minute: '2-digit' } : { day: 'numeric', month: 'short' });
  return (time: number) => format.format(new Date(time * 1000));
}

// chartMoment writes the time under a chart's crosshair, day and hour.
export function chartMoment(locale: Locale) {
  const format = new Intl.DateTimeFormat(dateLocale(locale), { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
  return (time: number) => format.format(new Date(time * 1000));
}
