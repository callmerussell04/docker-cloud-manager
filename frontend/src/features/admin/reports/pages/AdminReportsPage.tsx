import { useMemo, useState } from 'react';
import { Boxes, Cpu, Filter, HardDrive, MemoryStick, RefreshCcw, ShieldAlert, Users } from 'lucide-react';

import { Page } from '@/shared/ui/Page';
import { Toolbar, ToolbarButton } from '@/shared/ui/Toolbar';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { failureReason } from '@/shared/api/apiError';
import { dateLocale, useLocale, useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import type { UserUsageReportItem } from '../model';
import { useAuditEvents, useRefreshUsageSnapshots, useReportsOverview, useUserUsageReport, useUserUsageTimeline } from '../queries';
import { useDebouncedValue, useNowSeconds, usePageOf } from '@/shared/lib/hooks';
import { auditPageSize, numberValue, percentValue, rangeParams, ranges, userPageSize } from '../components/reportFormat';
import { ReportMetricCard } from '../components/ReportMetricCard';
import { ErrorState } from '@/shared/ui/ErrorState';
import { UserUsageTable } from '../components/UserUsageTable';
import { AuditEventsTable, type AuditFilterValues } from '../components/AuditEventsTable';
import { UserUsageChartModal } from '../components/UserUsageChartModal';

function currentSeconds() {
  return Math.floor(Date.now() / 1000);
}

export function AdminReportsPage() {
  const t = useT();
  const notify = useNotificationStore((state) => state.notify);
  const locale = useLocale();
  const [rangeSeconds, setRangeSeconds] = useState(ranges[1].seconds);
  const [sort, setSort] = useState('disk');
  const [userSearch, setUserSearch] = useState('');
  const [selectedUser, setSelectedUser] = useState<UserUsageReportItem | undefined>();
  const [auditFilters, setAuditFilters] = useState<AuditFilterValues>({ search: '', outcome: '', action: '', resourceType: '' });
  const debouncedUserSearch = useDebouncedValue(userSearch);
  const debouncedAuditSearch = useDebouncedValue(auditFilters.search);
  // The window ends when the range was chosen or usage was last refreshed,
  // so a refresh moves it forward and shows what happened since.
  const [windowEnd, setWindowEnd] = useState(currentSeconds);
  const params = useMemo(() => rangeParams(rangeSeconds, windowEnd), [rangeSeconds, windowEnd]);
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
    // Moving the window changes every report's query key, which reloads them.
    refreshSnapshots.mutate(undefined, {
      onSuccess: () => setWindowEnd(currentSeconds()),
      onError: (error) => notify(t('admin.reports.refreshFailed'), 'error', failureReason(error, t)),
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
    <Page
      toolbar={
        <Toolbar
          title={t('admin.reports.title')}
          actions={
            <ToolbarButton
              icon={RefreshCcw}
              label={t('admin.reports.refreshUsage')}
              busy={refreshSnapshots.isPending || overview.isFetching || users.isFetching || audit.isFetching}
              onClick={refetchAll}
            />
          }
        />
      }
    >
      <div className="flex justify-end">
        <div className="flex overflow-hidden rounded-lg border border-slate-200 dark:border-slate-700">
          {ranges.map((range) => (
            <button
              key={range.seconds}
              type="button"
              onClick={() => {
                setRangeSeconds(range.seconds);
                setWindowEnd(currentSeconds());
              }}
              className={`px-3 py-2 text-sm font-medium ${
                rangeSeconds === range.seconds
                  ? 'bg-accent-fill text-on-accent'
                  : 'bg-white text-slate-700 hover:bg-slate-50 dark:bg-slate-900 dark:text-slate-200 dark:hover:bg-slate-800'
              }`}
            >
              {range.label}
            </button>
          ))}
      </div>
      </div>

      {(overview.isError || users.isError) && (
        <ErrorState
          title={t('admin.reports.usageLoadFailed')}
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
          <ReportMetricCard title={t('admin.reports.auditEvents')} value={numberValue(overview.data?.audit_events_total ?? 0)} icon={ShieldAlert} />
          <ReportMetricCard title={t('admin.reports.failedActions')} value={numberValue(overview.data?.failed_actions_total ?? 0)} icon={Filter} />
          <ReportMetricCard title={t('admin.reports.activeUsers')} value={numberValue(overview.data?.active_users_total ?? 0)} icon={Users} />
          <ReportMetricCard title={t('admin.reports.actualMemory')} value={formatBytes(overview.data?.memory_usage_bytes ?? 0)} icon={MemoryStick} />
          <ReportMetricCard title={t('admin.reports.reservedMemory')} value={formatBytes(overview.data?.reserved_memory_bytes ?? 0)} icon={MemoryStick} />
          <ReportMetricCard title={t('admin.reports.cpu')} value={percentValue(overview.data?.cpu_percent ?? 0)} icon={Cpu} />
          <ReportMetricCard title={t('admin.reports.disk')} value={formatBytes(overview.data?.total_disk_bytes ?? 0)} icon={HardDrive} />
          <ReportMetricCard title={t('admin.reports.resourcesTotal')} value={numberValue(overview.data?.resources_total ?? 0)} icon={Boxes} />
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
    </Page>
  );
}
