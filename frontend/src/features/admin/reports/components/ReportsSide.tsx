import type { ReactNode } from 'react';
import type { UseQueryResult } from '@tanstack/react-query';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { ErrorState } from '@/shared/ui/ErrorState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { failureRate, type AuditEventsResponse, type ReportsOverview } from '../model';
import { EventLines } from './EventLines';
import { auditActionLabel, numberValue, percentValue } from './reportFormat';

function Section({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="grid min-w-0 content-start gap-3">
      <h2 id={id} className="text-[12px] font-semibold text-label-2">
        {title}
      </h2>
      {children}
    </section>
  );
}

function Figure({ label, value, sub }: { label: string; value: string; sub: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-[12px] text-label-2">{label}</dt>
      <dd className="text-[20px] font-[650] tabular-nums tracking-[-0.02em] text-label">{value}</dd>
      <dd className="text-[12px] text-label-2">{sub}</dd>
    </div>
  );
}

// TopActions is the range's most frequent actions as bars of one gray, the
// longest the most frequent, each with its count.
function TopActions({ overview }: { overview: ReportsOverview }) {
  const t = useT();
  const top = overview.top_actions ?? [];
  const most = Math.max(1, ...top.map((item) => item.count));
  if (top.length === 0) return <p className="text-label-2">{t('admin.reports.nothingInRange')}</p>;
  return (
    <ul className="grid gap-2">
      {top.map((item) => (
        <li key={item.action} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1">
          <span className="truncate text-label" title={item.action}>
            {auditActionLabel(t, item.action)}
          </span>
          <b className="text-right font-semibold tabular-nums text-label">{numberValue(item.count)}</b>
          <span aria-hidden className="col-span-2 h-1.5 overflow-hidden rounded-full bg-fill">
            <span className="block h-full rounded-full bg-label-3" style={{ width: `${(item.count / most) * 100}%` }} />
          </span>
        </li>
      ))}
    </ul>
  );
}

interface ReportsSideProps {
  overview: ReportsOverview;
  failures: UseQueryResult<AuditEventsResponse>;
}

// ReportsSide is the column beside the usage chart: the range's figures, the
// most frequent actions and the latest failures with their reasons.
export function ReportsSide({ overview, failures }: ReportsSideProps) {
  const t = useT();
  const bytes = (value: number) => formatBytes(value, 1);
  const rate = failureRate(overview.failed_actions_total, overview.audit_events_total);
  return (
    <div className="grid content-start gap-8">
      <Section id="reports-range" title={t('admin.reports.side.range')}>
        <dl className="grid grid-cols-2 gap-x-6 gap-y-4">
          <Figure
            label={t('admin.reports.figure.actions')}
            value={numberValue(overview.audit_events_total)}
            sub={
              <span className={overview.failed_actions_total > 0 ? 'text-red-text' : undefined}>
                {t('admin.reports.figure.actionsSub', {
                  failed: numberValue(overview.failed_actions_total),
                  rate: new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(rate),
                })}
              </span>
            }
          />
          <Figure label={t('admin.reports.figure.activeUsers')} value={numberValue(overview.active_users_total)} sub={t('admin.reports.figure.activeUsersSub')} />
          <Figure
            label={t('admin.reports.figure.used')}
            value={bytes(overview.memory_usage_bytes)}
            sub={t('admin.reports.figure.usedSub', { reserved: bytes(overview.reserved_memory_bytes) })}
          />
          <Figure label={t('admin.reports.figure.cpu')} value={percentValue(overview.cpu_percent)} sub={t('admin.reports.figure.cpuSub')} />
          <Figure label={t('admin.reports.figure.disk')} value={bytes(overview.total_disk_bytes)} sub={t('admin.reports.figure.diskSub')} />
          <Figure
            label={t('admin.reports.figure.resources')}
            value={numberValue(overview.resources_total)}
            sub={t('admin.reports.figure.resourcesSub', {
              containers: overview.containers_total,
              volumes: overview.volumes_total,
              images: overview.images_total,
              builds: overview.builds_total,
              projects: overview.projects_total,
            })}
          />
        </dl>
      </Section>
      <Section id="reports-top-actions" title={t('admin.reports.topActions')}>
        <TopActions overview={overview} />
      </Section>
      <Section id="reports-failures" title={t('admin.reports.recentFailures')}>
        {failures.isError ? (
          <ErrorState
            title={t('admin.reports.failuresLoadFailed')}
            message={failureReason(failures.error, t)}
            onRetry={() => failures.refetch()}
            isRetrying={failures.isFetching}
          />
        ) : !failures.data ? (
          <LoadingRows rows={3} dot={false} columns={[1]} />
        ) : failures.data.events.length === 0 ? (
          <p className="text-label-2">{t('admin.reports.noFailures')}</p>
        ) : (
          <EventLines events={failures.data?.events ?? []} />
        )}
      </Section>
    </div>
  );
}
