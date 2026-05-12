import { useMemo, useState } from 'react';
import { BarChart3, Clock, Database, Filter, HardDrive, MemoryStick, RefreshCcw, Search, ShieldAlert, Users, type LucideIcon } from 'lucide-react';
import {
  Bar,
  BarChart,
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { Button } from '@/components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
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

function rangeParams(seconds: number) {
  const to = Math.floor(Date.now() / 1000);
  return { from: to - seconds, to };
}

function numberValue(value: number) {
  return new Intl.NumberFormat().format(value);
}

function MetricCard({ title, value, icon: Icon }: { title: string; value: string; icon: LucideIcon }) {
  return (
    <Card>
      <CardContent className="p-5 flex items-center gap-4">
        <div className="w-11 h-11 rounded-xl bg-red-50 dark:bg-red-950/40 flex items-center justify-center shrink-0">
          <Icon className="w-5 h-5 text-red-600 dark:text-red-300" />
        </div>
        <div className="min-w-0">
          <p className="text-sm font-medium text-slate-500 dark:text-slate-400 truncate">{title}</p>
          <p className="text-2xl font-semibold text-slate-900 dark:text-slate-100 truncate">{value}</p>
        </div>
      </CardContent>
    </Card>
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
  const [selectedUser, setSelectedUser] = useState<UserUsageReportItem | undefined>();
  const [auditSearch, setAuditSearch] = useState('');
  const [auditOutcome, setAuditOutcome] = useState('');
  const [auditAction, setAuditAction] = useState('');
  const [auditResourceType, setAuditResourceType] = useState('');
  const params = useMemo(() => rangeParams(rangeSeconds), [rangeSeconds]);

  const overview = useReportsOverview(params);
  const users = useUserUsageReport({ ...params, sort, page: 1, limit: 10 });
  const selectedOwnerId = selectedUser?.owner_id || users.data?.users?.[0]?.owner_id;
  const timeline = useUserUsageTimeline(selectedOwnerId, params);
  const audit = useAuditEvents({ ...params, search: auditSearch, outcome: auditOutcome, action: auditAction, resource_type: auditResourceType, page: 1, limit: 20 });
  const refreshSnapshots = useRefreshUsageSnapshots();

  const currentUser = selectedUser ?? users.data?.users?.find((item) => item.owner_id === selectedOwnerId);
  const chartPoints = (timeline.data?.points ?? []).map((point) => ({
    time: new Date(point.bucket_start * 1000).toLocaleString(dateLocale(locale), { month: 'short', day: '2-digit', hour: '2-digit' }),
    memory: point.reserved_memory_bytes,
    disk: point.total_disk_bytes,
    actions: point.actions_total,
  }));
  const actionBars = overview.data?.top_actions?.map((item) => ({ action: item.action.replace('.', '\n'), count: item.count })) ?? [];
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

  return (
    <div className="space-y-6">
      <div className="flex flex-col lg:flex-row justify-between items-start lg:items-center gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-red-600 dark:text-red-400 flex items-center gap-3">
            <BarChart3 className="w-8 h-8" />
            {t('admin.reports.title')}
          </h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">{t('admin.reports.subtitle')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex rounded-lg border border-slate-200 dark:border-slate-700 overflow-hidden">
            {ranges.map((range) => (
              <button
                key={range.seconds}
                type="button"
                onClick={() => setRangeSeconds(range.seconds)}
                className={`px-3 py-2 text-sm font-medium ${rangeSeconds === range.seconds ? 'bg-red-600 text-white' : 'bg-white dark:bg-slate-900 text-slate-700 dark:text-slate-200'}`}
              >
                {range.label}
              </button>
            ))}
          </div>
          <Button variant="secondary" onClick={refetchAll} isLoading={refreshSnapshots.isPending || overview.isFetching || users.isFetching || audit.isFetching} className="px-3">
            <RefreshCcw className="w-4 h-4 mr-2" /> {t('admin.reports.refreshUsage')}
          </Button>
        </div>
      </div>

      <div className={`rounded-lg border px-4 py-3 text-sm ${isUsageStale ? 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200' : 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/60 dark:bg-emerald-950/30 dark:text-emerald-200'}`}>
        {t(isUsageStale ? 'admin.reports.usageSnapshotStale' : 'admin.reports.usageSnapshotFresh', { value: lastSnapshotText })}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
        <MetricCard title={t('admin.reports.auditEvents')} value={numberValue(overview.data?.audit_events_total ?? 0)} icon={ShieldAlert} />
        <MetricCard title={t('admin.reports.failedActions')} value={numberValue(overview.data?.failed_actions_total ?? 0)} icon={Filter} />
        <MetricCard title={t('admin.reports.memory')} value={formatBytes(overview.data?.reserved_memory_bytes ?? 0)} icon={MemoryStick} />
        <MetricCard title={t('admin.reports.disk')} value={formatBytes(overview.data?.total_disk_bytes ?? 0)} icon={HardDrive} />
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-3 gap-6">
        <Card className="xl:col-span-2">
          <CardHeader className="flex flex-row items-center justify-between gap-3">
            <CardTitle className="text-lg flex items-center gap-2">
              <Users className="w-5 h-5 text-red-500" />
              {t('admin.reports.topUsers')}
            </CardTitle>
            <select value={sort} onChange={(event) => setSort(event.target.value)} className="h-9 rounded-lg border border-slate-200 bg-white px-3 text-sm dark:border-slate-700 dark:bg-slate-900">
              <option value="disk">{t('admin.reports.sort.disk')}</option>
              <option value="memory">{t('admin.reports.sort.memory')}</option>
              <option value="actions">{t('admin.reports.sort.actions')}</option>
              <option value="containers">{t('admin.reports.sort.containers')}</option>
            </select>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="text-left text-slate-500 dark:text-slate-400">
                  <tr>
                    <th className="py-2 pr-3">{t('common.user')}</th>
                    <th className="py-2 pr-3">{t('admin.reports.memory')}</th>
                    <th className="py-2 pr-3">{t('admin.reports.disk')}</th>
                    <th className="py-2 pr-3">{t('admin.reports.actions')}</th>
                    <th className="py-2 pr-3">{t('admin.reports.containers')}</th>
                  </tr>
                </thead>
                <tbody>
                  {(users.data?.users ?? []).map((item) => (
                    <tr
                      key={item.owner_id}
                      onClick={() => setSelectedUser(item)}
                      className="border-t border-slate-100 dark:border-slate-800 cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-800/60"
                    >
                      <td className="py-3 pr-3 font-medium text-slate-900 dark:text-slate-100">{item.owner_username || item.owner_id}</td>
                      <td className="py-3 pr-3">{formatBytes(item.reserved_memory_bytes)}</td>
                      <td className="py-3 pr-3">{formatBytes(item.total_disk_bytes)}</td>
                      <td className="py-3 pr-3">{numberValue(item.actions_total)}</td>
                      <td className="py-3 pr-3">{item.containers_running}/{item.containers_total}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!users.isLoading && (users.data?.users.length ?? 0) === 0 && (
                <div className="py-10 text-center text-slate-500">{t('admin.reports.emptyUsers')}</div>
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-lg flex items-center gap-2">
              <Clock className="w-5 h-5 text-red-500" />
              {currentUser?.owner_username || t('admin.reports.userTrend')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-72">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={chartPoints}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="time" hide />
                  <YAxis tickFormatter={(value) => formatBytes(Number(value), 0)} width={72} />
                  <Tooltip formatter={(value, name) => [name === 'actions' ? numberValue(Number(value)) : formatBytes(Number(value)), String(name)]} />
                  <Line type="monotone" dataKey="disk" stroke="#dc2626" dot={false} strokeWidth={2} />
                  <Line type="monotone" dataKey="memory" stroke="#2563eb" dot={false} strokeWidth={2} />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-3 gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-lg flex items-center gap-2">
              <Database className="w-5 h-5 text-red-500" />
              {t('admin.reports.actionsOverTime')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-72">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={actionBars}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="action" hide />
                  <YAxis />
                  <Tooltip />
                  <Bar dataKey="count" fill="#dc2626" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>

        <Card className="xl:col-span-2">
          <CardHeader className="space-y-3">
            <CardTitle className="text-lg flex items-center gap-2">
              <ShieldAlert className="w-5 h-5 text-red-500" />
              {t('admin.reports.auditEventsTable')}
            </CardTitle>
            <div className="grid grid-cols-1 md:grid-cols-[1fr_160px_180px_160px] gap-3">
              <div className="relative">
                <Search className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-slate-400" />
                <input
                  value={auditSearch}
                  onChange={(event) => setAuditSearch(event.target.value)}
                  placeholder={t('common.search')}
                  className="w-full h-10 rounded-lg border border-slate-200 bg-white pl-9 pr-3 text-sm dark:border-slate-700 dark:bg-slate-900"
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
                      <tr key={event.id} className="border-t border-slate-100 align-top dark:border-slate-800">
                        <td className="py-3 pr-3 whitespace-nowrap">{new Date(event.occurred_at * 1000).toLocaleString(dateLocale(locale))}</td>
                        <td className="py-3 pr-3">{event.actor_username || event.actor_user_id || 'system'}</td>
                        <td className="py-3 pr-3 font-medium">{event.action}</td>
                        <td className="py-3 pr-3">{event.outcome}</td>
                        <td className="py-3 pr-3">{event.resource_name || event.resource_id || event.resource_type}</td>
                        <td className="py-3 pr-3 min-w-64">
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
        </Card>
      </div>
    </div>
  );
}
