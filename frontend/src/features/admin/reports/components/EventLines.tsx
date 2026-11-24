import { CheckCircle2, XCircle } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { auditFailureKey, type AuditEvent } from '../model';
import { resourceName, useActorName, useAge } from './eventFormat';
import { auditActionLabel } from './reportFormat';

interface EventLinesProps {
  events: readonly AuditEvent[];
  // showActor names who acted on each line; leave it off for one user's
  // own actions.
  showActor?: boolean;
}

// EventLines is a short list of actions, one line each: a mark for the
// outcome, who did what to which resource, a failure's reason, and when.
export function EventLines({ events, showActor = true }: EventLinesProps) {
  const t = useT();
  const actorName = useActorName();
  const age = useAge();
  if (events.length === 0) return <p className="py-1 text-label-2">{t('admin.reports.nothingInRange')}</p>;
  return (
    <ul className="grid gap-2">
      {events.map((event) => {
        const failed = event.outcome === 'failure';
        const Mark = failed ? XCircle : CheckCircle2;
        return (
          <li key={event.id} className="grid grid-cols-[16px_minmax(0,1fr)] gap-x-2">
            <Mark
              aria-label={t(failed ? 'admin.reports.outcome.failure' : 'admin.reports.outcome.success')}
              className={cn('mt-px h-4 w-4', failed ? 'text-red' : 'text-green')}
              strokeWidth={2}
            />
            <div className="min-w-0">
              <div className="truncate text-label">
                {showActor && <b className="font-semibold">{actorName(event)} </b>}
                {auditActionLabel(t, event.action)}
                <span className="text-label-2"> · {resourceName(event)}</span>
              </div>
              <div className="truncate text-[12px] text-label-2">
                {failed && <span className="text-red-text">{t(auditFailureKey(event.error_code))} · </span>}
                {age(event.occurred_at)}
              </div>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
