import { CheckCircle2, XCircle } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { auditFailureKey, type AuditEvent } from '../model';
import { resourceName, useActorName, useShortAge } from './eventFormat';
import { auditActionLabel } from './reportFormat';

interface EventLinesProps {
  events: readonly AuditEvent[];
  // showActor names who acted on each line; leave it off for one user's
  // own actions.
  showActor?: boolean;
}

// EventLines is a short list of actions, one line each, parted by hairlines:
// a mark for the outcome, who did what to which resource and a failure's
// reason, and how long ago.
export function EventLines({ events, showActor = true }: EventLinesProps) {
  const t = useT();
  const actorName = useActorName();
  const age = useShortAge();
  if (events.length === 0) return <p className="px-0.5 py-1.5 text-label-2">{t('admin.reports.nothingInRange')}</p>;
  return (
    <ul className="grid">
      {events.map((event) => {
        const failed = event.outcome === 'failure';
        const Mark = failed ? XCircle : CheckCircle2;
        const reason = failed ? t(auditFailureKey(event.error_code)) : undefined;
        const action = auditActionLabel(t, event.action);
        return (
          <li
            key={event.id}
            className="grid min-h-9 grid-cols-[16px_minmax(0,1fr)_auto] items-center gap-2.5 border-t border-separator text-[12.5px] first:border-t-0"
          >
            <Mark
              aria-label={t(failed ? 'admin.reports.outcome.failure' : 'admin.reports.outcome.success')}
              className={cn('h-3.5 w-3.5', failed ? 'text-red-text' : 'text-green')}
              strokeWidth={2}
            />
            <span className="truncate" title={[showActor ? actorName(event) : '', action, resourceName(event), reason].filter(Boolean).join(' · ')}>
              {showActor && <b className="font-semibold text-label">{actorName(event)} </b>}
              <span className="text-label-2">{action}</span> <span className="text-label">{resourceName(event)}</span>
              {reason && <span className="text-red-text"> · {reason}</span>}
            </span>
            <span className="tabular-nums text-label-2">{age(event.occurred_at)}</span>
          </li>
        );
      })}
    </ul>
  );
}
