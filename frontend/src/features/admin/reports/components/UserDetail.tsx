import type { UseQueryResult } from '@tanstack/react-query';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { cn, formatBytes } from '@/shared/lib/utils';
import { ErrorState } from '@/shared/ui/ErrorState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { StackedAreaChart } from '@/shared/ui/StackedAreaChart';
import { usageSeries, type AuditEventsResponse, type UsageMeasure, type UserTimeline } from '../model';
import { EventLines } from './EventLines';
import { numberValue, percentValue } from './reportFormat';

const measures: readonly UsageMeasure[] = ['reserved', 'used', 'cpu', 'disk'];

interface UserDetailProps {
  name: string;
  color: string;
  // actionsTotal is how many actions the user took in the range.
  actionsTotal: number;
  timeline: UserTimeline | undefined;
  // timelineFailed says the user's history could not be read.
  timelineFailed: boolean;
  onRetry: () => void;
  slotSeconds: number;
  from: number;
  to: number;
  rangeLabel: string;
  formatTime: (time: number) => string;
  formatMoment: (time: number) => string;
  // compact lays the charts in one column.
  compact: boolean;
  actions: UseQueryResult<AuditEventsResponse>;
}

// UserDetail is one user's usage over the range: memory reserved, memory in
// use, CPU and disk as small charts in their colour, each titled with its
// latest figure, then their recent actions.
export function UserDetail(props: UserDetailProps) {
  const { name, color, actionsTotal, timeline, timelineFailed, onRetry, slotSeconds, from, to, rangeLabel, formatTime, formatMoment, compact, actions } = props;
  const t = useT();
  const format = (measure: UsageMeasure) => (value: number) => (measure === 'cpu' ? percentValue(value) : formatBytes(value, 1));
  return (
    <div className="grid gap-[18px]">
      {timeline ? (
        <div className={cn('grid gap-x-[22px] gap-y-3.5', compact ? 'grid-cols-1' : 'grid-cols-2')}>
          {measures.map((measure) => {
            const series = usageSeries([timeline], measure, slotSeconds);
            const values = series.values.get(timeline.ownerId) ?? [];
            const latest = values[values.length - 1];
            const title = t(`admin.reports.measure.${measure}`);
            return (
              <section key={measure} className="min-w-0">
                <div className="mb-1 ml-11 flex items-baseline justify-between gap-3 text-[12px] font-semibold text-label-2">
                  <h3 className="truncate">{title}</h3>
                  <b className="shrink-0 text-[14px] tabular-nums text-label">{latest === undefined ? '—' : format(measure)(latest)}</b>
                </div>
                <StackedAreaChart
                  label={t('admin.reports.userChartLabel', { measure: title, user: name, range: rangeLabel })}
                  times={series.times}
                  from={from}
                  to={to}
                  series={[{ id: timeline.ownerId, label: name, color, values }]}
                  format={format(measure)}
                  scale={measure === 'cpu' ? 'plain' : 'bytes'}
                  formatTick={measure === 'cpu' ? percentValue : (value) => formatBytes(value, 2)}
                  formatTime={formatTime}
                  formatMoment={formatMoment}
                  height={130}
                  legend={false}
                />
              </section>
            );
          })}
        </div>
      ) : timelineFailed ? (
        <ErrorState title={t('admin.reports.usageLoadFailed')} onRetry={onRetry} />
      ) : (
        <LoadingRows rows={2} dot={false} columns={[1, 1]} />
      )}
      <section aria-labelledby="report-user-actions">
        <div className="flex items-baseline justify-between gap-3 px-0.5 pb-2.5">
          <h3 id="report-user-actions" className="text-[15px] font-[650] tracking-[-0.012em] text-label">
            {t('admin.reports.recentActionsOf', { user: name })}
          </h3>
          <span className="text-label-2">{t('admin.reports.inRange', { count: numberValue(actionsTotal) })}</span>
        </div>
        {actions.isError ? (
          <ErrorState title={t('admin.reports.auditLoadFailed')} message={failureReason(actions.error, t)} onRetry={() => actions.refetch()} isRetrying={actions.isFetching} />
        ) : actions.isLoading ? (
          <LoadingRows rows={3} dot={false} columns={[1]} />
        ) : (
          <EventLines events={actions.data?.events ?? []} showActor={false} />
        )}
      </section>
    </div>
  );
}
