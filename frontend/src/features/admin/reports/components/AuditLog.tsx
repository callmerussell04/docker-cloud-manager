import { dateLocale, useLocale, useT } from '@/shared/i18n';
import { List, ListHead, ListRow, type ListColumn } from '@/shared/ui/List';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Dot } from '@/shared/ui/StatusDot';
import { auditDetails, auditFailureKey, type AuditEvent } from '../model';
import { resourceName, useActorName, useAge } from './eventFormat';
import { auditActionLabel } from './reportFormat';

// The log's grid, shared by its head and rows; on compact content a row is
// the action over who, what and when.
const template = 'minmax(84px,0.6fr) minmax(90px,0.8fr) minmax(140px,1.3fr) minmax(110px,1fr) minmax(150px,1.3fr)';
const compactTemplate = 'minmax(0,1fr)';

type AuditColumn = 'time' | 'user' | 'action' | 'resource' | 'outcome';

// AuditHead is the log's column labels. The log is newest first, as the
// server keeps it, so they do not sort.
export function AuditHead() {
  const t = useT();
  const columns: ListColumn<AuditColumn>[] = [
    { key: 'time', label: t('admin.reports.column.time') },
    { key: 'user', label: t('admin.reports.column.user') },
    { key: 'action', label: t('admin.reports.column.action') },
    { key: 'resource', label: t('admin.reports.column.resource') },
    { key: 'outcome', label: t('admin.reports.column.outcome') },
  ];
  return <ListHead columns={columns} template={template} />;
}

// detailsTitle lists an event's recorded details for its tooltip.
function detailsTitle(event: AuditEvent) {
  return auditDetails(event)
    .map((detail) => `${detail.key}: ${detail.value}`)
    .join('\n');
}

// AuditLog is the audit events, one row each: when, who, the action by its
// localized name, the resource, and the outcome, a failure with its reason.
export function AuditLog({ events }: { events: readonly AuditEvent[] }) {
  const t = useT();
  const locale = useLocale();
  const compact = useShellLayout().rowsCompact;
  const actorName = useActorName();
  const age = useAge();
  const fullTime = new Intl.DateTimeFormat(dateLocale(locale), { dateStyle: 'medium', timeStyle: 'medium' });

  return (
    <List label={t('admin.reports.tab.audit')}>
      {events.map((event) => {
        const failed = event.outcome === 'failure';
        const reason = failed ? t(auditFailureKey(event.error_code)) : undefined;
        const when = fullTime.format(new Date(event.occurred_at * 1000));
        const details = detailsTitle(event);
        if (compact) {
          return (
            <ListRow key={event.id} template={compactTemplate}>
              <div className="min-w-0">
                <div className="flex min-w-0 items-center gap-2">
                  <Dot tone={failed ? 'bad' : 'ok'} />
                  <span className="truncate font-semibold text-label">{auditActionLabel(t, event.action)}</span>
                </div>
                <div className="truncate pl-4 text-[12px] text-label-2" title={when}>
                  {actorName(event)} · {resourceName(event)} · {age(event.occurred_at)}
                </div>
                {reason && <div className="truncate pl-4 text-[12px] text-red-text">{reason}</div>}
              </div>
            </ListRow>
          );
        }
        return (
          <ListRow key={event.id} template={template}>
            <div className="flex min-w-0 items-center gap-3.5">
              <Dot tone={failed ? 'bad' : 'ok'} />
              <span className="truncate tabular-nums text-label-2" title={when}>
                {age(event.occurred_at)}
              </span>
            </div>
            <span className="truncate text-label">{actorName(event)}</span>
            <span className="truncate font-medium text-label" title={event.action}>
              {auditActionLabel(t, event.action)}
            </span>
            <span className="truncate text-label-2" title={details || undefined}>
              {resourceName(event)}
            </span>
            {reason ? (
              <span className="truncate text-red-text" title={reason}>
                {reason}
              </span>
            ) : (
              <span className="truncate text-label-2">{t('admin.reports.outcome.success')}</span>
            )}
          </ListRow>
        );
      })}
    </List>
  );
}
