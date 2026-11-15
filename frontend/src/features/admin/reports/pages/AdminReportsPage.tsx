import { useMemo, useState } from 'react';
import { BarChart3, Boxes, Cpu, Filter, HardDrive, MemoryStick, RefreshCcw, ShieldAlert, Users } from 'lucide-react';

import { Button } from '@/shared/ui/Button';
import { dateLocale, useLocale, useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import type { UserUsageReportItem } from '../model';
import { useAuditEvents, useRefreshUsageSnapshots, useReportsOverview, useUserUsageReport, useUserUsageTimeline } from '../queries';
import { useDebouncedValue, useNowSeconds, usePageOf } from '../hooks';
import { auditPageSize, numberValue, percentValue, rangeParams, ranges, userPageSize } from '../components/reportFormat';
import { ReportMetricCard } from '../components/ReportMetricCard';
import { UsageErrorBanner } from '../components/UsageErrorBanner';
import { UserUsageTable } from '../components/UserUsageTable';
import { AuditEventsTable, type AuditFilterValues } from '../components/AuditEventsTable';
import { UserUsageChartModal } from '../components/UserUsageChartModal';

export function AdminReportsPage() {
  const t = useT();
  const locale = useLocale();
  const [rangeSeconds, setRangeSeconds] = useState(ranges[1].seconds);
  const [sort, setSort] = useState('disk');
  const [userSearch, setUserSearch] = useState('');
  const [selectedUser, setSelectedUser] = useState<UserUsageReportItem | undefined>();
  const [auditFilters, setAuditFilters] = useState<AuditFilterValues>({ search: '', outcome: '', action: '', resourceType: '' });
  const debouncedUserSearch = useDebouncedValue(userSearch);
  const debouncedAuditSearch = useDebouncedValue(auditFilters.search);
  const params = useMemo(() => rangeParams(rangeSeconds), [rangeSeconds]);
  const [userPage, setUserPage] = usePageOf([debouncedUserSearch, rangeSeconds, sort]);
  const [auditPage, setAuditPage] = usePageOf([debouncedAuditSearch, auditFilters.outcome, auditFilters.action, auditFilters.resourceType, rangeSeconds]);

  const overview = useReportsOverview(params);
  const users = useUserUsageReport({ ...params, sort, search: debouncedUserSearch, page: userPage, limit: userPageSize });
  const timeline = useUserUsageTimeline(selectedUser?.owner_id, params, Boolean(selectedUser));
  const audit = useAuditEvents({
    ...params,
    search: debouncedAuditSearch,
    outcome: auditFilters.outcome,
    action: auditFilters.action,
    resource_type: auditFilters.resourceType,
    page: auditPage,
    limit: auditPageSize,
  });
  const refreshSnapshots = useRefreshUsageSnapshots();
  const now = useNowSeconds();

  const lastSnapshotAt = overview.data?.last_usage_snapshot_at ?? 0;
  const snapshotInterval = overview.data?.usage_snapshot_interval_seconds ?? 0;
  const snapshotAge = lastSnapshotAt > 0 ? now - lastSnapshotAt : 0;
  const isUsageStale = lastSnapshotAt === 0 || (snapshotInterval > 0 && snapshotAge > snapshotInterval);
  const lastSnapshotText = lastSnapshotAt > 0
    ? new Date(lastSnapshotAt * 1000).toLocaleString(dateLocale(locale))
    : t('admin.reports.noUsageSnapshot');

  const refetchAll = () => {
    refreshSnapshots.mutate(undefined, {
      onSuccess: () => {
        overview.refetch();
        users.refetch();
        timeline.refetch();
        audit.refetch();
      },
    });
  };
  const retryUsage = () => {
    overview.refetch();
    users.refetch();
    if (selectedUser) {
      timeline.refetch();
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-col items-start justify-between gap-4 lg:flex-row lg:items-center">
        <div>
          <h1 className="flex items-center gap-3 text-3xl font-bold tracking-tight text-slate-900 dark:text-slate-100">
            <BarChart3 className="h-8 w-8 text-indigo-600 dark:text-indigo-300" />
            {t('admin.reports.title')}
          </h1>
          <p className="mt-1 text-slate-500 dark:text-slate-400">{t('admin.reports.subtitle')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex overflow-hidden rounded-lg border border-slate-200 dark:border-slate-700">
            {ranges.map((range) => (
              <button
                key={range.seconds}
                type="button"
                onClick={() => setRangeSeconds(range.seconds)}
                className={`px-3 py-2 text-sm font-medium ${
                  rangeSeconds === range.seconds
                    ? 'bg-indigo-600 text-white'
                    : 'bg-white text-slate-700 hover:bg-slate-50 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800'
                }`}
              >
                {range.label}
              </button>
            ))}
          </div>
          <Button variant="secondary" onClick={refetchAll} isLoading={refreshSnapshots.isPending || overview.isFetching || users.isFetching || audit.isFetching} className="px-3">
            <RefreshCcw className="mr-2 h-4 w-4" /> {t('admin.reports.refreshUsage')}
          </Button>
        </div>
      </div>

      {(overview.isError || users.isError) && (
        <UsageErrorBanner
          message={t('admin.reports.usageLoadFailed')}
          onRetry={retryUsage}
          isRetrying={overview.isFetching || users.isFetching || timeline.isFetching}
        />
      )}

      {!overview.isError && (
        <div className={`rounded-lg border px-4 py-3 text-sm ${isUsageStale ? 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200' : 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/60 dark:bg-emerald-950/30 dark:text-emerald-200'}`}>
          {t(isUsageStale ? 'admin.reports.usageSnapshotStale' : 'admin.reports.usageSnapshotFresh', { value: lastSnapshotText })}
        </div>
      )}

      {!overview.isError && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
          <ReportMetricCard title={t('admin.reports.auditEvents')} value={numberValue(overview.data?.audit_events_total ?? 0)} icon={ShieldAlert} tone="indigo" />
          <ReportMetricCard title={t('admin.reports.failedActions')} value={numberValue(overview.data?.failed_actions_total ?? 0)} icon={Filter} tone="amber" />
          <ReportMetricCard title={t('admin.reports.activeUsers')} value={numberValue(overview.data?.active_users_total ?? 0)} icon={Users} tone="sky" />
          <ReportMetricCard title={t('admin.reports.actualMemory')} value={formatBytes(overview.data?.memory_usage_bytes ?? 0)} icon={MemoryStick} tone="emerald" />
          <ReportMetricCard title={t('admin.reports.reservedMemory')} value={formatBytes(overview.data?.reserved_memory_bytes ?? 0)} icon={MemoryStick} tone="sky" />
          <ReportMetricCard title={t('admin.reports.cpu')} value={percentValue(overview.data?.cpu_percent ?? 0)} icon={Cpu} tone="indigo" />
          <ReportMetricCard title={t('admin.reports.disk')} value={formatBytes(overview.data?.total_disk_bytes ?? 0)} icon={HardDrive} tone="emerald" />
          <ReportMetricCard title={t('admin.reports.resourcesTotal')} value={numberValue(overview.data?.resources_total ?? 0)} icon={Boxes} tone="slate" />
        </div>
      )}

      <UserUsageTable
        query={users}
        search={userSearch}
        onSearchChange={setUserSearch}
        sort={sort}
        onSortChange={setSort}
        page={userPage}
        onPageChange={setUserPage}
        onSelect={setSelectedUser}
      />

      <AuditEventsTable
        query={audit}
        filters={auditFilters}
        onFilterChange={(name, value) => setAuditFilters((current) => ({ ...current, [name]: value }))}
        page={auditPage}
        onPageChange={setAuditPage}
      />

      <UserUsageChartModal user={selectedUser} query={timeline} onClose={() => setSelectedUser(undefined)} />
    </div>
  );
}
