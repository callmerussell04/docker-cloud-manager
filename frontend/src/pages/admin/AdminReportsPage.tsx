import { useEffect, useMemo, useState } from 'react';
import {
  Activity,
  AlertTriangle,
  BarChart3,
  Boxes,
  Cpu,
  Filter,
  HardDrive,
  MemoryStick,
  RefreshCcw,
  Search,
  ShieldAlert,
  Users,
  type LucideIcon,
} from 'lucide-react';
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

import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Modal } from '@/components/ui/Modal';
import { Pagination } from '@/components/ui/Pagination';
import { useAuditEvents, useRefreshUsageSnapshots, useReportsOverview, useUserUsageReport, useUserUsageTimeline } from '@/features/reports/hooks';
import type { AuditEvent, UserUsageReportItem } from '@/features/reports/types';
import { dateLocale, useLocale, useT } from '@/lib/i18n';
import { formatBytes } from '@/lib/utils';

const ranges = [
  { label: '24h', seconds: 24 * 60 * 60 },
  { label: '7d', seconds: 7 * 24 * 60 * 60 },
  { label: '30d', seconds: 30 * 24 * 60 * 60 },
  { label: '90d', seconds: 90 * 24 * 60 * 60 },
];

const auditActions = [
  'container.create',
  'container.start',
  'container.stop',
  'container.delete',
  'container.expose',
  'volume.create',
  'volume.delete',
  'image.delete',
  'build.create_archive',
  'build.create_git',
  'build.cancel',
  'build.delete',
  'compose.upload_deploy',
  'compose.git_deploy',
  'project.start',
  'project.stop',
  'project.cancel',
  'project.delete',
  'auth.register',
  'auth.login',
  'auth.oidc_callback',
  'user.admin_create',
  'user.admin_update',
  'user.admin_deactivate',
  'user.admin_reactivate',
  'telemetry.logs_ticket',
  'telemetry.terminal_ticket',
];

const auditResourceTypes = ['container', 'volume', 'image', 'build', 'project', 'user', 'auth', 'telemetry', 'system'];
const userPageSize = 10;
const auditPageSize = 20;

function rangeParams(seconds: number) {
  const to = Math.floor(Date.now() / 1000);
  return { from: to - seconds, to };
}

function numberValue(value: number) {
  return new Intl.NumberFormat().format(value);
}

function percentValue(value: number) {
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value)}%`;
}

function chartSeriesLabel(t: ReturnType<typeof useT>, name: string) {
  switch (name) {
    case 'actualMemory':
      return t('admin.reports.chart.actualMemory');
    case 'reservedMemory':
      return t('admin.reports.chart.reservedMemory');
    case 'disk':
      return t('admin.reports.chart.disk');
    case 'cpu':
      return t('admin.reports.chart.cpu');
    default:
      return name;
  }
}

function chartTooltipValue(value: unknown, name: unknown, item: { dataKey?: unknown }, t: ReturnType<typeof useT>) {
  const dataKey = String(item.dataKey ?? name);
  const numericValue = Number(value);
  return [
    dataKey === 'cpu' ? percentValue(numericValue) : formatBytes(numericValue),
    chartSeriesLabel(t, dataKey),
  ];
}

function useDebouncedValue<T>(value: T, delayMs = 350) {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delayMs);
    return () => window.clearTimeout(timer);
  }, [delayMs, value]);

  return debounced;
}

function MetricCard({
  title,
  value,
  icon: Icon,
  tone = 'sky',
}: {
  title: string;
  value: string;
  icon: LucideIcon;
  tone?: 'sky' | 'indigo' | 'emerald' | 'amber' | 'slate';
}) {
  const toneClass = {
    sky: 'bg-sky-50 text-sky-700 dark:bg-sky-950/40 dark:text-sky-300',
    indigo: 'bg-indigo-50 text-indigo-700 dark:bg-indigo-950/40 dark:text-indigo-300',
    emerald: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300',
    amber: 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300',
    slate: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300',
  }[tone];

  return (
    <Card>
      <CardContent className="flex items-center gap-4 p-5">
        <div className={`flex h-11 w-11 shrink-0 items-center justify-center rounded-lg ${toneClass}`}>
          <Icon className="h-5 w-5" />
        </div>
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-slate-500 dark:text-slate-400">{title}</p>
          <p className="truncate text-2xl font-semibold text-slate-900 dark:text-slate-100">{value}</p>
        </div>
      </CardContent>
    </Card>
  );
}

function UsageErrorBanner({
  message,
  onRetry,
  isRetrying,
}: {
  message: string;
  onRetry: () => void;
  isRetrying: boolean;
}) {
  const t = useT();

  return (
    <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-start gap-2">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{message}</span>
        </div>
        <Button type="button" variant="secondary" onClick={onRetry} isLoading={isRetrying} className="h-9 px-3">
          <RefreshCcw className="mr-2 h-4 w-4" />
          {t('common.refresh')}
        </Button>
      </div>
    </div>
  );
}

function auditDetails(event: AuditEvent) {
  if (!event.details_json || event.details_json === '{}') {
    return [];
  }
  try {
    const parsed = JSON.parse(event.details_json) as Record<string, unknown>;
    const details: Array<[string, unknown]> = [
      ['full_domain', parsed.full_domain],
      ['domain_prefix', parsed.domain_prefix],
      ['internal_port', parsed.internal_port],
      ['container_name', parsed.container_name],
      ['container_id', parsed.container_id],
      ['project_name', parsed.project_name],
      ['project_id', parsed.project_id],
      ['compose_service', parsed.compose_service],
      ['previous_domain_prefix', parsed.previous_domain_prefix],
      ['previous_internal_port', parsed.previous_internal_port],
      ['source_type', parsed.source_type],
    ];
    return details
      .filter(([, value]) => value !== undefined && value !== null && String(value) !== '')
      .map(([key, value]) => ({ key, value: String(value) }));
  } catch {
    return [{ key: 'details', value: event.details_json }];
  }
}

export function AdminReportsPage() {
  const t = useT();
  const locale = useLocale();
  const [rangeSeconds, setRangeSeconds] = useState(ranges[1].seconds);
  const [sort, setSort] = useState('disk');
  const [userSearch, setUserSearch] = useState('');
  const [userPage, setUserPage] = useState(1);
  const [selectedUser, setSelectedUser] = useState<UserUsageReportItem | undefined>();
  const [auditSearch, setAuditSearch] = useState('');
  const [auditOutcome, setAuditOutcome] = useState('');
  const [auditAction, setAuditAction] = useState('');
  const [auditResourceType, setAuditResourceType] = useState('');
  const [auditPage, setAuditPage] = useState(1);
  const debouncedUserSearch = useDebouncedValue(userSearch);
  const debouncedAuditSearch = useDebouncedValue(auditSearch);
  const params = useMemo(() => rangeParams(rangeSeconds), [rangeSeconds]);

  const overview = useReportsOverview(params);
  const users = useUserUsageReport({ ...params, sort, search: debouncedUserSearch, page: userPage, limit: userPageSize });
  const timeline = useUserUsageTimeline(selectedUser?.owner_id, params, Boolean(selectedUser));
  const audit = useAuditEvents({
    ...params,
    search: debouncedAuditSearch,
    outcome: auditOutcome,
    action: auditAction,
    resource_type: auditResourceType,
    page: auditPage,
    limit: auditPageSize,
  });
  const refreshSnapshots = useRefreshUsageSnapshots();

  useEffect(() => {
    setUserPage(1);
  }, [debouncedUserSearch, rangeSeconds, sort]);

  useEffect(() => {
    setAuditPage(1);
  }, [debouncedAuditSearch, auditOutcome, auditAction, auditResourceType, rangeSeconds]);

  const chartPoints = (timeline.data?.points ?? []).map((point) => ({
    time: new Date(point.bucket_start * 1000).toLocaleString(dateLocale(locale), { month: 'short', day: '2-digit', hour: '2-digit' }),
    actualMemory: point.memory_usage_bytes,
    reservedMemory: point.reserved_memory_bytes,
    disk: point.total_disk_bytes,
    cpu: point.cpu_percent,
  }));
  const lastSnapshotAt = overview.data?.last_usage_snapshot_at ?? 0;
  const snapshotInterval = overview.data?.usage_snapshot_interval_seconds ?? 0;
  const snapshotAge = lastSnapshotAt > 0 ? Math.floor(Date.now() / 1000) - lastSnapshotAt : 0;
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
          <MetricCard title={t('admin.reports.auditEvents')} value={numberValue(overview.data?.audit_events_total ?? 0)} icon={ShieldAlert} tone="indigo" />
          <MetricCard title={t('admin.reports.failedActions')} value={numberValue(overview.data?.failed_actions_total ?? 0)} icon={Filter} tone="amber" />
          <MetricCard title={t('admin.reports.activeUsers')} value={numberValue(overview.data?.active_users_total ?? 0)} icon={Users} tone="sky" />
          <MetricCard title={t('admin.reports.actualMemory')} value={formatBytes(overview.data?.memory_usage_bytes ?? 0)} icon={MemoryStick} tone="emerald" />
          <MetricCard title={t('admin.reports.reservedMemory')} value={formatBytes(overview.data?.reserved_memory_bytes ?? 0)} icon={MemoryStick} tone="sky" />
          <MetricCard title={t('admin.reports.cpu')} value={percentValue(overview.data?.cpu_percent ?? 0)} icon={Cpu} tone="indigo" />
          <MetricCard title={t('admin.reports.disk')} value={formatBytes(overview.data?.total_disk_bytes ?? 0)} icon={HardDrive} tone="emerald" />
          <MetricCard title={t('admin.reports.resourcesTotal')} value={numberValue(overview.data?.resources_total ?? 0)} icon={Boxes} tone="slate" />
        </div>
      )}

      <Card>
        <CardHeader className="space-y-3">
          <div className="flex flex-col justify-between gap-3 lg:flex-row lg:items-center">
            <CardTitle className="flex items-center gap-2 text-lg">
              <Users className="h-5 w-5 text-sky-500" />
              {t('admin.reports.topUsers')}
            </CardTitle>
            <div className="grid w-full grid-cols-1 gap-3 sm:grid-cols-[minmax(220px,1fr)_220px] lg:w-auto">
              <div className="relative">
                <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
                <input
                  value={userSearch}
                  onChange={(event) => setUserSearch(event.target.value)}
                  placeholder={t('common.search')}
                  className="h-10 w-full rounded-lg border border-slate-200 bg-white pl-9 pr-3 text-sm dark:border-slate-700 dark:bg-slate-900"
                />
              </div>
              <select value={sort} onChange={(event) => setSort(event.target.value)} className="h-10 rounded-lg border border-slate-200 bg-white px-3 text-sm dark:border-slate-700 dark:bg-slate-900">
                <option value="disk">{t('admin.reports.sort.disk')}</option>
                <option value="actual_memory">{t('admin.reports.sort.actualMemory')}</option>
                <option value="reserved_memory">{t('admin.reports.sort.reservedMemory')}</option>
                <option value="cpu">{t('admin.reports.sort.cpu')}</option>
                <option value="resources">{t('admin.reports.sort.resources')}</option>
                <option value="actions">{t('admin.reports.sort.actions')}</option>
                <option value="containers">{t('admin.reports.sort.containers')}</option>
              </select>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-slate-500 dark:text-slate-400">
                <tr>
                  <th className="py-2 pr-3">{t('common.user')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.actualMemory')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.reservedMemory')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.cpu')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.disk')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.containers')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.volumes')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.images')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.builds')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.projects')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.actions')}</th>
                </tr>
              </thead>
              <tbody>
                {!users.isError && (users.data?.users ?? []).map((item) => {
                  return (
                    <tr
                      key={item.owner_id}
                      onClick={() => setSelectedUser(item)}
                      className="cursor-pointer border-t border-slate-100 hover:bg-slate-50 dark:border-slate-800 dark:hover:bg-slate-800/60"
                    >
                      <td className="py-3 pr-3 font-medium text-slate-900 dark:text-slate-100">{item.owner_username || item.owner_id}</td>
                      <td className="py-3 pr-3">{formatBytes(item.memory_usage_bytes)}</td>
                      <td className="py-3 pr-3">{formatBytes(item.reserved_memory_bytes)}</td>
                      <td className="py-3 pr-3">{percentValue(item.cpu_percent)}</td>
                      <td className="py-3 pr-3">{formatBytes(item.total_disk_bytes)}</td>
                      <td className="py-3 pr-3">{item.containers_running}/{item.containers_total}</td>
                      <td className="py-3 pr-3">{numberValue(item.volumes_total)}</td>
                      <td className="py-3 pr-3">{numberValue(item.images_total)}</td>
                      <td className="py-3 pr-3">{numberValue(item.builds_total)}</td>
                      <td className="py-3 pr-3">{numberValue(item.projects_total)}</td>
                      <td className="py-3 pr-3">{numberValue(item.actions_total)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {users.isError && (
              <div className="py-6">
                <UsageErrorBanner
                  message={t('admin.reports.usersLoadFailed')}
                  onRetry={() => users.refetch()}
                  isRetrying={users.isFetching}
                />
              </div>
            )}
            {!users.isError && !users.isLoading && (users.data?.users.length ?? 0) === 0 && (
              <div className="py-10 text-center text-slate-500">{t('admin.reports.emptyUsers')}</div>
            )}
          </div>
        </CardContent>
        {!users.isError && <Pagination currentPage={userPage} totalItems={users.data?.total_count ?? 0} pageSize={userPageSize} onPageChange={setUserPage} />}
      </Card>

      <Card>
        <CardHeader className="space-y-3">
          <CardTitle className="flex items-center gap-2 text-lg">
            <Activity className="h-5 w-5 text-indigo-500" />
            {t('admin.reports.auditEventsTable')}
          </CardTitle>
          <div className="grid grid-cols-1 gap-3 md:grid-cols-[1fr_160px_180px_160px]">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-400" />
              <input
                value={auditSearch}
                onChange={(event) => setAuditSearch(event.target.value)}
                placeholder={t('common.search')}
                className="h-10 w-full rounded-lg border border-slate-200 bg-white pl-9 pr-3 text-sm dark:border-slate-700 dark:bg-slate-900"
              />
            </div>
            <select value={auditOutcome} onChange={(event) => setAuditOutcome(event.target.value)} className="h-10 rounded-lg border border-slate-200 bg-white px-3 text-sm dark:border-slate-700 dark:bg-slate-900">
              <option value="">{t('admin.reports.allOutcomes')}</option>
              <option value="success">{t('admin.reports.outcome.success')}</option>
              <option value="failure">{t('admin.reports.outcome.failure')}</option>
            </select>
            <select value={auditAction} onChange={(event) => setAuditAction(event.target.value)} className="h-10 rounded-lg border border-slate-200 bg-white px-3 text-sm dark:border-slate-700 dark:bg-slate-900">
              <option value="">{t('admin.reports.allActions')}</option>
              {auditActions.map((action) => (
                <option key={action} value={action}>{action}</option>
              ))}
            </select>
            <select value={auditResourceType} onChange={(event) => setAuditResourceType(event.target.value)} className="h-10 rounded-lg border border-slate-200 bg-white px-3 text-sm dark:border-slate-700 dark:bg-slate-900">
              <option value="">{t('admin.reports.allResources')}</option>
              {auditResourceTypes.map((resourceType) => (
                <option key={resourceType} value={resourceType}>{resourceType}</option>
              ))}
            </select>
          </div>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-slate-500 dark:text-slate-400">
                <tr>
                  <th className="py-2 pr-3">{t('admin.reports.time')}</th>
                  <th className="py-2 pr-3">{t('common.user')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.action')}</th>
                  <th className="py-2 pr-3">{t('common.status')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.resource')}</th>
                  <th className="py-2 pr-3">{t('admin.reports.details')}</th>
                </tr>
              </thead>
              <tbody>
                {(audit.data?.events ?? []).map((event) => {
                  const details = auditDetails(event);
                  return (
                    <tr key={event.id} className="align-top border-t border-slate-100 dark:border-slate-800">
                      <td className="whitespace-nowrap py-3 pr-3">{new Date(event.occurred_at * 1000).toLocaleString(dateLocale(locale))}</td>
                      <td className="py-3 pr-3">{event.actor_username || event.actor_user_id || 'system'}</td>
                      <td className="py-3 pr-3 font-medium">{event.action}</td>
                      <td className="py-3 pr-3">{event.outcome}</td>
                      <td className="py-3 pr-3">{event.resource_name || event.resource_id || event.resource_type}</td>
                      <td className="min-w-64 py-3 pr-3">
                        {details.length > 0 ? (
                          <div className="flex flex-wrap gap-1.5">
                            {details.map((detail) => (
                              <span key={`${event.id}-${detail.key}`} className="max-w-72 truncate rounded border border-slate-200 px-2 py-1 text-xs text-slate-600 dark:border-slate-700 dark:text-slate-300" title={`${detail.key}: ${detail.value}`}>
                                {detail.key}: {detail.value}
                              </span>
                            ))}
                          </div>
                        ) : (
                          <span className="text-slate-400">-</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
            {!audit.isLoading && (audit.data?.events.length ?? 0) === 0 && (
              <div className="py-10 text-center text-slate-500">{t('admin.reports.emptyAudit')}</div>
            )}
          </div>
        </CardContent>
        <Pagination currentPage={auditPage} totalItems={audit.data?.total_count ?? 0} pageSize={auditPageSize} onPageChange={setAuditPage} />
      </Card>

      <Modal
        isOpen={Boolean(selectedUser)}
        onClose={() => setSelectedUser(undefined)}
        title={selectedUser?.owner_username || selectedUser?.owner_id || t('admin.reports.userTrend')}
        className="max-w-6xl"
      >
        <div className="space-y-6">
          {timeline.isError && (
            <UsageErrorBanner
              message={t('admin.reports.timelineLoadFailed')}
              onRetry={() => timeline.refetch()}
              isRetrying={timeline.isFetching}
            />
          )}
          {!timeline.isError && !timeline.isLoading && (timeline.data?.points.length ?? 0) === 0 && (
            <div className="rounded-lg border border-slate-200 px-4 py-10 text-center text-sm text-slate-500 dark:border-slate-700 dark:text-slate-400">
              {t('admin.reports.emptyTimeline')}
            </div>
          )}
          {!timeline.isError && (
            <div className="h-[520px]">
              <ResponsiveContainer key={selectedUser?.owner_id} width="100%" height="100%">
                <LineChart data={chartPoints}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="time" minTickGap={24} />
                  <YAxis yAxisId="bytes" tickFormatter={(value) => formatBytes(Number(value), 0)} width={76} />
                  <YAxis yAxisId="cpu" orientation="right" tickFormatter={(value) => percentValue(Number(value))} width={64} />
                  <Tooltip formatter={(value, name, item) => chartTooltipValue(value, name, item, t)} />
                  <Legend formatter={(value) => chartSeriesLabel(t, String(value))} />
                  <Line yAxisId="bytes" name="actualMemory" type="monotone" dataKey="actualMemory" stroke="#10b981" dot={false} strokeWidth={2} />
                  <Line yAxisId="bytes" name="reservedMemory" type="monotone" dataKey="reservedMemory" stroke="#0ea5e9" dot={false} strokeWidth={2} />
                  <Line yAxisId="bytes" name="disk" type="monotone" dataKey="disk" stroke="#6366f1" dot={false} strokeWidth={2} />
                  <Line yAxisId="cpu" name="cpu" type="monotone" dataKey="cpu" stroke="#f59e0b" dot={false} strokeWidth={2} />
                </LineChart>
              </ResponsiveContainer>
            </div>
          )}
        </div>
      </Modal>
    </div>
  );
}
