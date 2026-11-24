import { CheckCircle2, XCircle } from 'lucide-react';
import { dateLocale, useLocale, useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { auditDetails, auditFailureKey, type AuditEvent } from '../model';
import { resourceName, useActorName, useAge } from './eventFormat';
import { auditActionLabel, auditDetailLabel } from './reportFormat';

// The log's grid: when, who, the action, the resource with its details, and
// the outcome. A compact row is the action and resource over who and when,
// beside the outcome.
const template = '80px 90px 160px minmax(0,1fr) minmax(0,1.2fr)';
const compactTemplate = 'minmax(0,1fr) auto';

// AuditLog is the audit events, newest first, one row each, parted by
// hairlines. Under each resource are the details recorded with the action
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
          <span className={cn('inline-flex min-w-0 items-center gap-1.5', failed ? 'text-red-text' : 'text-label-2')}>
            <Mark aria-hidden className={cn('h-3.5 w-3.5 shrink-0', failed ? 'text-red' : 'text-green')} strokeWidth={2} />
            <span className="truncate" title={outcome}>
              {outcome}
            </span>
          </span>
        );
        return (
          <li
            key={event.id}
            className={cn('grid min-h-11 items-center gap-3.5 px-3.5 py-1', index > 0 && 'border-t border-separator')}
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
                <span className="truncate text-label-2" title={action}>
                  {action}
                </span>
                <span className="grid min-w-0 py-1">
                  <span className="truncate text-label">{resourceName(event)}</span>
                  {details && (
                    <span className="truncate text-[12px] text-label-2" title={details}>
                      {details}
                    </span>
                  )}
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
