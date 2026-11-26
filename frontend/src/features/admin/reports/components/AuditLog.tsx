import { CheckCircle2, XCircle } from 'lucide-react';
import { dateLocale, useLocale, useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { auditDetails, auditFailureKey, type AuditEvent } from '../model';
import { resourceName, useActorName, useAge } from './eventFormat';
import { auditActionLabel, auditDetailLabel } from './reportFormat';

// The log's grid, shared by its head and rows: when, who, the action, the
// resource, the details recorded with it, and the outcome. A compact row is
// the action and resource over the details and who and when, beside the
// outcome.
const template = '92px minmax(80px,0.7fr) minmax(130px,1fr) minmax(110px,1fr) minmax(150px,1.4fr) minmax(130px,1fr)';
const compactTemplate = 'minmax(0,1fr) auto';

// AuditHead is the log's column labels, for Page's head so they stay under
// the toolbar. The log is newest first, as the server keeps it, so they do
// not sort. Compact rows have no columns, so it shows nothing then.
export function AuditHead() {
  const t = useT();
  if (useShellLayout().rowsCompact) return null;
  const labels = ['time', 'user', 'action', 'resource', 'details', 'outcome'] as const;
  return (
    <div className="grid h-[34px] items-center gap-4 px-3.5 shadow-[inset_0_-1px_0_var(--separator)]" style={{ gridTemplateColumns: template }}>
      {labels.map((label) => (
        <span key={label} className="truncate text-[11.5px] font-[650] text-label-2">
          {t(`admin.reports.column.${label}`)}
        </span>
      ))}
    </div>
  );
}

// AuditLog is the audit events, newest first, one row each, parted by
// hairlines; details and a failure's reason wrap to two lines. The details
// column holds what was recorded with the action
// (the address and port a container was published on, the project and
// service, what it was before), each by its name; a failure shows its
// reason.
export function AuditLog({ events }: { events: readonly AuditEvent[] }) {
  const t = useT();
  const locale = useLocale();
  const compact = useShellLayout().rowsCompact;
  const actorName = useActorName();
  const age = useAge();
  const fullTime = new Intl.DateTimeFormat(dateLocale(locale), { dateStyle: 'medium', timeStyle: 'medium' });

  return (
    <ul aria-label={t('admin.reports.tab.audit')} className="grid">
      {events.map((event, index) => {
        const failed = event.outcome === 'failure';
        const Mark = failed ? XCircle : CheckCircle2;
        const outcome = failed ? t(auditFailureKey(event.error_code)) : t('admin.reports.outcome.success');
        const when = fullTime.format(new Date(event.occurred_at * 1000));
        const details = auditDetails(event)
          .map((detail) => `${auditDetailLabel(t, detail.key)} ${detail.value}`)
          .join(' · ');
        const action = auditActionLabel(t, event.action);
        const outcomeCell = (
          <span className={cn('inline-flex min-w-0 items-start gap-1.5', failed ? 'text-red-text' : 'text-label-2')}>
            <Mark aria-hidden className={cn('mt-[2px] h-3.5 w-3.5 shrink-0', failed ? 'text-red' : 'text-green')} strokeWidth={2} />
            <span className="line-clamp-2" title={outcome}>
              {outcome}
            </span>
          </span>
        );
        return (
          <li
            key={event.id}
            className={cn('grid min-h-11 items-center gap-4 px-3.5 py-1', index > 0 && 'border-t border-separator')}
            style={{ gridTemplateColumns: compact ? compactTemplate : template }}
          >
            {compact ? (
              <>
                <span className="grid min-w-0 py-1">
                  <span className="truncate">
                    <span className="text-label-2">{action}</span> <span className="text-label">{resourceName(event)}</span>
                  </span>
                  {details && <span className="truncate text-[12px] text-label-2">{details}</span>}
                  <span className="truncate text-[12px] text-label-2" title={when}>
                    <b className="font-semibold text-label">{actorName(event)}</b> · {age(event.occurred_at)}
                  </span>
                </span>
                {outcomeCell}
              </>
            ) : (
              <>
                <span className="truncate tabular-nums text-label-2" title={when}>
                  {age(event.occurred_at)}
                </span>
                <span className="truncate font-semibold text-label">{actorName(event)}</span>
                <span className="truncate text-label" title={action}>
                  {action}
                </span>
                <span className="truncate text-label" title={resourceName(event)}>
                  {resourceName(event)}
                </span>
                <span className="line-clamp-2 text-label-2" title={details || undefined}>
                  {details}
                </span>
                {outcomeCell}
              </>
            )}
          </li>
        );
      })}
    </ul>
  );
}
