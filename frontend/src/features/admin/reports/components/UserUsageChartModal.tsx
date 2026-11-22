import type { ReactNode } from 'react';
import { Cpu, HardDrive, MemoryStick, type LucideIcon } from 'lucide-react';
import type { UseQueryResult } from '@tanstack/react-query';
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { Modal } from '@/shared/ui/Modal';
import { dateLocale, useLocale, useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import type { UserUsageReportItem, UserUsageTimelineResponse } from '../model';
import { chartSeriesLabel, chartTooltipValue, percentValue } from './reportFormat';
import { UsageErrorBanner } from './UsageErrorBanner';

function UsageChartPanel({
  title,
  icon: Icon,
  children,
}: {
  title: string;
  icon: LucideIcon;
  children: ReactNode;
}) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white/70 p-4 dark:border-slate-700 dark:bg-slate-900/50">
      <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-slate-700 dark:text-slate-200">
        <Icon className="h-4 w-4 text-label-2" />
        {title}
      </div>
      <div className="h-64">
        {children}
      </div>
    </div>
  );
}

interface UserUsageChartModalProps {
  user: UserUsageReportItem | undefined;
  query: UseQueryResult<UserUsageTimelineResponse>;
  onClose: () => void;
}

/** The selected user's CPU, memory and disk usage over the report range. */
export function UserUsageChartModal({ user, query, onClose }: UserUsageChartModalProps) {
  const t = useT();
  const locale = useLocale();
  const chartPoints = (query.data?.points ?? []).map((point) => ({
    time: new Date(point.bucket_start * 1000).toLocaleString(dateLocale(locale), { month: 'short', day: '2-digit', hour: '2-digit' }),
    actualMemory: point.memory_usage_bytes,
    reservedMemory: point.reserved_memory_bytes,
    disk: point.total_disk_bytes,
    cpu: point.cpu_percent,
  }));

  return (
    <Modal
      isOpen={Boolean(user)}
      onClose={onClose}
      title={user?.owner_username || user?.owner_id || t('admin.reports.userTrend')}
      className="max-w-6xl"
    >
      <div className="space-y-6">
        {query.isError && (
          <UsageErrorBanner
            message={t('admin.reports.timelineLoadFailed')}
            onRetry={() => query.refetch()}
            isRetrying={query.isFetching}
          />
        )}
        {!query.isError && !query.isLoading && (query.data?.points.length ?? 0) === 0 && (
          <div className="rounded-lg border border-slate-200 px-4 py-10 text-center text-sm text-slate-500 dark:border-slate-700 dark:text-slate-400">
            {t('admin.reports.emptyTimeline')}
          </div>
        )}
        {!query.isError && (
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
            <UsageChartPanel title={t('admin.reports.cpu')} icon={Cpu}>
              <ResponsiveContainer key={`${user?.owner_id}-cpu`} width="100%" height="100%">
                <LineChart data={chartPoints}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="time" minTickGap={24} />
                  <YAxis tickFormatter={(value) => percentValue(Number(value))} width={64} />
                  <Tooltip formatter={(value, name, item) => chartTooltipValue(value, name, item, t)} />
                  <Legend formatter={(value) => chartSeriesLabel(t, String(value))} />
                  <Line name="cpu" type="monotone" dataKey="cpu" stroke="var(--series-4)" dot={false} strokeWidth={2} />
                </LineChart>
              </ResponsiveContainer>
            </UsageChartPanel>

            <UsageChartPanel title={t('admin.reports.memory')} icon={MemoryStick}>
              <ResponsiveContainer key={`${user?.owner_id}-memory`} width="100%" height="100%">
                <LineChart data={chartPoints}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="time" minTickGap={24} />
                  <YAxis tickFormatter={(value) => formatBytes(Number(value), 0)} width={76} />
                  <Tooltip formatter={(value, name, item) => chartTooltipValue(value, name, item, t)} />
                  <Legend formatter={(value) => chartSeriesLabel(t, String(value))} />
                  <Line name="actualMemory" type="monotone" dataKey="actualMemory" stroke="var(--series-3)" dot={false} strokeWidth={2} />
                  <Line name="reservedMemory" type="monotone" dataKey="reservedMemory" stroke="var(--series-1)" dot={false} strokeWidth={2} />
                </LineChart>
              </ResponsiveContainer>
            </UsageChartPanel>

            <UsageChartPanel title={t('admin.reports.disk')} icon={HardDrive}>
              <ResponsiveContainer key={`${user?.owner_id}-disk`} width="100%" height="100%">
                <LineChart data={chartPoints}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="time" minTickGap={24} />
                  <YAxis tickFormatter={(value) => formatBytes(Number(value), 0)} width={76} />
                  <Tooltip formatter={(value, name, item) => chartTooltipValue(value, name, item, t)} />
                  <Legend formatter={(value) => chartSeriesLabel(t, String(value))} />
                  <Line name="disk" type="monotone" dataKey="disk" stroke="var(--series-5)" dot={false} strokeWidth={2} />
                </LineChart>
              </ResponsiveContainer>
            </UsageChartPanel>
          </div>
        )}
      </div>
    </Modal>
  );
}
