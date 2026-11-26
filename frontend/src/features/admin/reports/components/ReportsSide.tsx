import type { ReactNode } from 'react';
import type { UseQueryResult } from '@tanstack/react-query';
import { useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { ErrorState } from '@/shared/ui/ErrorState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { failureRate, type AuditEventsResponse, type ReportsOverview } from '../model';
import { EventLines } from './EventLines';
import { auditActionLabel, numberValue, percentValue } from './reportFormat';

// Tile is one panel of the column: a label over its content.
function Tile({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="flex min-w-0 flex-col gap-2.5 rounded-[22px] bg-tile p-[18px] shadow-[inset_0_0_0_0.5px_var(--separator)]">
      <h2 id={id} className="text-[12px] font-semibold text-label-2">
        {title}
      </h2>
      {children}
    </section>
  );
}

// Figure is one of the range's numbers, on a tile of its own: its label, the
// figure, and what it is of.
function Figure({ label, value, sub }: { label: string; value: string; sub: ReactNode }) {
  return (
    <div className="grid min-w-0 content-start gap-[3px] rounded-[16px] bg-tile px-3 py-2.5 shadow-[inset_0_0_0_0.5px_var(--separator)]">
      <dt className="truncate text-[12px] font-semibold text-label-2">{label}</dt>
      <dd className="text-[18px] font-[650] tabular-nums tracking-[-0.02em] text-label">{value}</dd>
      <dd className="text-[12px] leading-[1.4] text-label-2">{sub}</dd>
    </div>
  );
}

// TopActions is the range's most frequent actions as bars, the longest the
// most frequent, each with its count.
function TopActions({ overview }: { overview: ReportsOverview }) {
  const t = useT();
  const top = overview.top_actions ?? [];
  const most = Math.max(1, ...top.map((item) => item.count));
  if (top.length === 0) return <p className="text-label-2">{t('admin.reports.nothingInRange')}</p>;
  return (
    <ul className="grid gap-2">
      {top.map((item) => {
        const label = auditActionLabel(t, item.action);
        return (
          <li key={item.action} className="grid grid-cols-[110px_minmax(0,1fr)_34px] items-center gap-3 text-[12.5px]" title={`${label}: ${item.count}`}>
            <span className="truncate text-label">{label}</span>
            <span aria-hidden className="h-2 overflow-hidden rounded-r bg-fill">
              <span className="block h-full rounded-r bg-accent-fill" style={{ width: `${(item.count / most) * 100}%` }} />
            </span>
            <b className="text-right font-semibold tabular-nums text-label">{numberValue(item.count)}</b>
          </li>
        );
      })}
    </ul>
  );
}

interface ReportsSideProps {
  overview: ReportsOverview;
  failures: UseQueryResult<AuditEventsResponse>;
}

// ReportsSide is the column beside the usage chart, as three tiles: the
// range's figures, the most frequent actions and the latest failures with
// their reasons.
export function ReportsSide({ overview, failures }: ReportsSideProps) {
  const t = useT();
  const bytes = (value: number) => formatBytes(value, 1);
  const rate = failureRate(overview.failed_actions_total, overview.audit_events_total);
  return (
    <div className="grid content-start gap-5">
      <Tile id="reports-range" title={t('admin.reports.side.range')}>
        <dl className="grid grid-cols-2 gap-2">
          <Figure
            label={t('admin.reports.figure.actions')}
            value={numberValue(overview.audit_events_total)}
            sub={
              <>
                <b className={overview.failed_actions_total > 0 ? 'font-semibold text-red-text' : 'font-normal'}>
                  {t('admin.reports.figure.failed', { count: numberValue(overview.failed_actions_total) })}
                </b>
                {` · ${percentValue(rate)}`}
              </>
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
      </Tile>
      <Tile id="reports-top-actions" title={t('admin.reports.topActions')}>
        <TopActions overview={overview} />
      </Tile>
      <Tile id="reports-failures" title={t('admin.reports.recentFailures')}>
        {failures.isError ? (
          <ErrorState
            compact
            title={t('admin.reports.failuresLoadFailed')}
            error={failures.error}
            onRetry={() => failures.refetch()}
            isRetrying={failures.isFetching}
          />
        ) : !failures.data ? (
          <LoadingRows rows={3} dot={false} columns={[1]} />
        ) : failures.data.events.length === 0 ? (
          <p className="text-label-2">{t('admin.reports.noFailures')}</p>
        ) : (
          <EventLines events={failures.data.events} />
        )}
      </Tile>
    </div>
  );
}
