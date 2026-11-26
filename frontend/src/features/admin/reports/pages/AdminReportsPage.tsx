import { useMemo, useState, type ReactNode } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Loader2, RefreshCcw } from 'lucide-react';

import { failureReason } from '@/shared/api/apiError';
import { useLocale, useT } from '@/shared/i18n';
import { ageOf } from '@/shared/lib/age';
import { useDebouncedValue, useElementWidth, useListState, useNowSeconds } from '@/shared/lib/hooks';
import type { ListStateSpec } from '@/shared/lib/listState';
import { formatBytes } from '@/shared/lib/utils';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorState } from '@/shared/ui/ErrorState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { Page } from '@/shared/ui/Page';
import { Pagination } from '@/shared/ui/Pagination';
import { Pills } from '@/shared/ui/Pills';
import { StackedAreaChart } from '@/shared/ui/StackedAreaChart';
import { Toolbar, ToolbarSearch } from '@/shared/ui/Toolbar';
import { AuditHead, AuditLog } from '../components/AuditLog';
import { chartMoment, chartTime, rangeParams } from '../components/reportFormat';
import { ReportsSide } from '../components/ReportsSide';
import { UserDetail } from '../components/UserDetail';
import { UsersTable, usersTableMinWidth } from '../components/UsersTable';
import {
  auditLogStart,
  defaultReportRange,
  defaultUsageSort,
  nextUsageSort,
  rangeSeconds,
  readReportRange,
  reportRanges,
  sortUsageUsers,
  trendChange,
  usageSeries,
  usageSlotSeconds,
  usageSortKeys,
  userSeries,
  type ReportRange,
  type ReportsOverview,
  type UsageSortKey,
} from '../model';
import { useAuditEvents, useRefreshUsageSnapshots, useReportsOverview, useUserUsageReport, useUserUsageTimelines } from '../queries';
import { useNotificationStore } from '@/shared/ui/notificationStore';

// The page's state in the URL: the report (?tab=audit; Usage is the default
// and left out), the range (?range=24h; 7 days left out), the user whose
// usage is open (?user=<id>), and each report's list state.
type Tab = 'usage' | 'audit';
const param = { tab: 'tab', range: 'range', user: 'user' } as const;

const usageListSpec: ListStateSpec<'all', UsageSortKey> = { filters: ['all'], sortKeys: usageSortKeys, defaultSort: defaultUsageSort };
type AuditFilter = 'all' | 'failed';
const auditListSpec: ListStateSpec<AuditFilter, 'time'> = { filters: ['all', 'failed'], sortKeys: ['time'], defaultSort: { key: 'time', ascending: false } };

// The page reads the first usersLimit users (the server's page maximum), each
// with their timeline; past that the table says how many it shows.
const usersLimit = 100;
const auditPageSize = 50;
// The audit log's placeholder rows: when, who, what, the resource and its details.
const auditShape = [0.6, 0.8, 1.3, 1, 1.3];
const recentFailuresLimit = 6;
const userActionsLimit = 8;
// The side column, 320 px with a 20 px gap, sits beside the chart once the
// content leaves the users table its full width beside it.
const sideColumn = 320;
const sideGap = 20;
const sideColumnWidth = usersTableMinWidth + sideColumn + sideGap;

function currentSeconds() {
  return Math.floor(Date.now() / 1000);
}

// useReportWindow is the range's window. It ends when the range was chosen
// or usage was last collected, so collecting moves it forward and every
// report reloads with what happened since, keeping the previous figures on
// screen meanwhile.
function useReportWindow(range: ReportRange) {
  const [held, setHeld] = useState(() => ({ range, end: currentSeconds() }));
  // Adjusting state while rendering, as React documents for state that
  // follows a changed input.
  if (held.range !== range) setHeld({ range, end: currentSeconds() });
  const params = useMemo(() => rangeParams(rangeSeconds[held.range], held.end), [held]);
  const moveToNow = () => setHeld({ range, end: currentSeconds() });
  return [params, moveToNow] as const;
}

interface FreshnessProps {
  overview: ReportsOverview;
  collecting: boolean;
  onCollect: () => void;
}

// Freshness says when usage was last collected and how often, says it may be
// out of date once it is older than two intervals, and offers to collect it
// now.
function Freshness({ overview, collecting, onCollect }: FreshnessProps) {
  const t = useT();
  const now = useNowSeconds();
  const ageText = (seconds: number) => {
    const age = ageOf(0, seconds);
    return t(`common.age.${age.unit}`, { count: age.count });
  };
  const last = overview.last_usage_snapshot_at;
  const interval = overview.usage_snapshot_interval_seconds;
  const stale = last === 0 || (interval > 0 && now - last > 2 * interval);
  const text = last > 0 ? t('admin.reports.collected', { age: ageText(now - last), interval: ageText(interval) }) : t('admin.reports.notCollected');
  return (
    <div className="flex min-w-0 flex-wrap items-center justify-end gap-x-3.5 gap-y-1 text-label-2">
      <span>
        {text}
        {stale && last > 0 && ` · ${t('admin.reports.stale')}`}
      </span>
      <button type="button" onClick={onCollect} disabled={collecting} className="inline-flex items-center gap-1 whitespace-nowrap rounded font-[550] text-accent hover:underline disabled:opacity-60">
        {collecting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCcw className="h-3.5 w-3.5" />}
        {t('admin.reports.collectNow')}
      </button>
    </div>
  );
}

// AdminReportsPage is two reports behind toolbar pills. Usage: memory
// reserved by each user over the range as one stacked chart, the users table
// with a trend per row (a row opens that user's charts and recent actions),
// and a column with the range's figures, the most frequent actions and the
// latest failures. Audit log: every action, searchable and filterable to
// failures, each failure with its reason.
export function AdminReportsPage() {
  const t = useT();
  const locale = useLocale();
  const notify = useNotificationStore((state) => state.notify);
  const [params, setParams] = useSearchParams();
  const tab: Tab = params.get(param.tab) === 'audit' ? 'audit' : 'usage';
  const range = readReportRange(params.get(param.range));
  const openUser = params.get(param.user);
  const [usageView, changeUsage] = useListState(usageListSpec);
  const [auditView, changeAudit] = useListState(auditListSpec);
  const [reportWindow, moveWindowToNow] = useReportWindow(range);
  const [measure, contentWidth] = useElementWidth<HTMLDivElement>(1000);

  const usage = tab === 'usage';
  const overview = useReportsOverview(reportWindow, usage);
  const users = useUserUsageReport({ ...reportWindow, sort: 'reserved_memory', page: 1, limit: usersLimit }, usage);
  const usersList = useMemo(() => users.data?.users ?? [], [users.data]);
  const ownerIds = useMemo(() => usersList.map((user) => user.owner_id), [usersList]);
  // Colours are numbered over everyone with usage in the last 30 days, not
  // the range shown, so changing the range never repaints anyone. On the
  // 30-day range this is the same query as the users'.
  const colourUsers = useUserUsageReport(
    { ...rangeParams(rangeSeconds['30d'], reportWindow.to), sort: 'reserved_memory', page: 1, limit: usersLimit },
    usage,
  );
  const timelines = useUserUsageTimelines(ownerIds, reportWindow, usage);
  const failures = useAuditEvents({ ...reportWindow, outcome: 'failure', page: 1, limit: recentFailuresLimit }, usage);
  const openRow = usersList.find((user) => user.owner_id === openUser);
  const userActions = useAuditEvents({ ...reportWindow, actor_user_id: openRow?.owner_id, page: 1, limit: userActionsLimit }, usage && Boolean(openRow));
  const auditSearch = useDebouncedValue(auditView.search);
  // The audit log is the whole log, newest first; it has no range.
  const audit = useAuditEvents(
    {
      from: auditLogStart,
      search: auditSearch.trim() || undefined,
      outcome: auditView.filter === 'failed' ? 'failure' : undefined,
      page: auditView.page,
      limit: auditPageSize,
    },
    tab === 'audit',
  );
  const refresh = useRefreshUsageSnapshots();

  const setParam = (name: string, value: string | null, also?: (next: URLSearchParams) => void) =>
    setParams((current) => {
      const next = new URLSearchParams(current);
      if (value === null) next.delete(name);
      else next.set(name, value);
      also?.(next);
      return next;
    }, { replace: true });

  // Switching reports starts the other one fresh, in the same range.
  const changeTab = (next: Tab) =>
    setParams(() => {
      const updated = new URLSearchParams();
      if (next === 'audit') updated.set(param.tab, 'audit');
      if (range !== defaultReportRange) updated.set(param.range, range);
      return updated;
    }, { replace: true });
  const changeRange = (next: ReportRange) => setParam(param.range, next === defaultReportRange ? null : next, (updated) => updated.delete('page'));
  const toggleUser = (ownerId: string) => setParam(param.user, openUser === ownerId ? null : ownerId);

  const collect = () =>
    refresh.mutate(undefined, {
      onSuccess: moveWindowToNow,
      onError: (error) => notify(t('admin.reports.refreshFailed'), 'error', failureReason(error, t)),
    });

  const rangeLabel = t(`admin.reports.range.${range}`);
  const formatTime = chartTime(locale, range);
  const formatMoment = chartMoment(locale);
  const bytes = (value: number) => formatBytes(value, 1);

  const pills = (
    <div className="flex items-center gap-2">
      <Pills
        label={t('admin.reports.tabsLabel')}
        value={tab}
        onChange={changeTab}
        items={[
          { id: 'usage', label: t('admin.reports.tab.usage') },
          { id: 'audit', label: t('admin.reports.tab.audit') },
        ]}
      />
      {tab === 'usage' && (
        <Pills label={t('admin.reports.rangeLabel')} value={range} onChange={changeRange} items={reportRanges.map((id) => ({ id, label: t(`admin.reports.range.${id}`) }))} />
      )}
      {tab === 'audit' && (
        <>
          <Pills
            label={t('admin.reports.filterLabel')}
            value={auditView.filter}
            onChange={(filter) => changeAudit({ filter, page: 1 })}
            items={[
              { id: 'all', label: t('admin.reports.filter.all') },
              { id: 'failed', label: t('admin.reports.filter.failed') },
            ]}
          />
          <ToolbarSearch value={auditView.search} onChange={(search) => changeAudit({ search, page: 1 })} label={t('admin.reports.searchAudit')} />
        </>
      )}
    </div>
  );

  let content: ReactNode;
  let head: ReactNode;
  if (tab === 'audit') {
    const events = audit.data?.events ?? [];
    if (audit.isError && !audit.data) {
      content = <ErrorState columns={auditShape} title={t('admin.reports.auditLoadFailed')} error={audit.error} onRetry={() => audit.refetch()} isRetrying={audit.isFetching} />;
    } else if (audit.isLoading) {
      content = <LoadingRows rows={8} columns={auditShape} />;
    } else if (events.length === 0) {
      content = auditSearch.trim() ? (
        <NoMatchesState query={auditSearch} onClear={() => changeAudit({ search: '', page: 1 })} description={t('admin.reports.auditNoMatches')} />
      ) : (
        <EmptyState columns={auditShape} title={t(auditView.filter === 'failed' ? 'admin.reports.emptyFailed' : 'admin.reports.emptyAudit')} />
      );
    } else {
      head = <AuditHead />;
      content = (
        <>
          <AuditLog events={events} />
          <Pagination currentPage={auditView.page} pageSize={auditPageSize} totalItems={audit.data?.total_count ?? 0} onPageChange={(page) => changeAudit({ page })} />
        </>
      );
    }
  } else if ((overview.isError && !overview.data) || (users.isError && !users.data)) {
    content = (
      <ErrorState
        title={t('admin.reports.usageLoadFailed')}
        error={overview.error ?? users.error}
        onRetry={() => {
          void overview.refetch();
          void users.refetch();
        }}
        isRetrying={overview.isFetching || users.isFetching}
      />
    );
  } else if (!overview.data || !users.data || (!colourUsers.data && !colourUsers.isError)) {
    content = <LoadingRows rows={6} dot={false} columns={[1.5, 1, 0.6, 0.6, 0.6]} />;
  } else if (usersList.length === 0) {
    content = (
      <EmptyState
        title={t('admin.reports.emptyUsers')}
        description={t('admin.reports.emptyUsersHint')}
        action={{ label: t('admin.reports.collectNow'), onClick: collect, icon: RefreshCcw, busy: refresh.isPending }}
      />
    );
  } else {
    // Each user keeps one colour, by the order of their ids among everyone
    // with usage in the last 30 days, and the chart stacks them in that same
    // order whatever the table's sort.
    const seriesOf = userSeries([...(colourUsers.data?.users.map((user) => user.owner_id) ?? []), ...ownerIds]);
    const colorOf = (ownerId: string) => `var(--series-${seriesOf.get(ownerId) ?? 1})`;
    const slotSeconds = usageSlotSeconds(rangeSeconds[range], overview.data.usage_snapshot_interval_seconds);
    const reserved = usageSeries(timelines.timelines, 'reserved', slotSeconds);
    const trendOf = (ownerId: string) => reserved.values.get(ownerId) ?? [];
    const trends = new Map(ownerIds.map((ownerId) => [ownerId, trendChange(trendOf(ownerId))]));
    const byId = new Map(usersList.map((user) => [user.owner_id, user]));
    const nameOf = (ownerId: string) => byId.get(ownerId)?.owner_username || ownerId;
    const stacked = [...seriesOf.keys()]
      .filter((ownerId) => reserved.values.has(ownerId))
      .map((ownerId) => ({ id: ownerId, label: nameOf(ownerId), color: colorOf(ownerId), values: reserved.values.get(ownerId) ?? [] }));
    const side = contentWidth >= sideColumnWidth;
    const mainWidth = side ? contentWidth - sideColumn - sideGap : contentWidth;
    const openTimeline = timelines.timelines.find((timeline) => timeline.ownerId === openRow?.owner_id);

    content = (
      <div className={side ? 'grid grid-cols-[minmax(0,1fr)_320px] items-start gap-5' : 'grid gap-5'}>
        <div className="grid min-w-0 content-start gap-5">
          <Freshness overview={overview.data} collecting={refresh.isPending} onCollect={collect} />
          {timelines.failed && (
            <p className="text-red-text">
              {t('admin.reports.timelinesFailed')}{' '}
              <button type="button" onClick={timelines.refetch} className="font-medium text-accent hover:underline">
                {t('common.tryAgain')}
              </button>
            </p>
          )}
          {timelines.isLoading && timelines.timelines.length === 0 ? (
            <LoadingRows rows={4} dot={false} columns={[1]} />
          ) : (
            <StackedAreaChart
              label={t('admin.reports.chartLabel', { range: rangeLabel })}
              times={reserved.times}
              from={reportWindow.from}
              to={reportWindow.to}
              series={stacked}
              format={bytes}
              scale="bytes"
              formatTick={(value) => formatBytes(value, 2)}
              formatTime={formatTime}
              formatMoment={formatMoment}
              height={mainWidth < 600 ? 200 : 220}
              endLabels
              totalLabel={t('admin.reports.total')}
            />
          )}
          {users.data.total_count > usersList.length && (
            <p className="text-label-2">{t('admin.reports.shownUsers', { shown: usersList.length, total: users.data.total_count })}</p>
          )}
          <UsersTable
            users={sortUsageUsers(usersList, usageView.sort, trends)}
            sort={usageView.sort}
            onSort={(key) => changeUsage({ sort: nextUsageSort(usageView.sort, key) })}
            rangeLabel={rangeLabel}
            colorOf={colorOf}
            trendOf={trendOf}
            openUser={openRow?.owner_id ?? null}
            onToggle={toggleUser}
            detail={
              openRow && (
                <UserDetail
                  name={nameOf(openRow.owner_id)}
                  color={colorOf(openRow.owner_id)}
                  actionsTotal={openRow.actions_total}
                  compact={mainWidth < 560}
                  timeline={openTimeline}
                  timelineFailed={timelines.failed && !openTimeline}
                  onRetry={timelines.refetch}
                  slotSeconds={slotSeconds}
                  from={reportWindow.from}
                  to={reportWindow.to}
                  rangeLabel={rangeLabel}
                  formatTime={formatTime}
                  formatMoment={formatMoment}
                  actions={userActions}
                />
              )
            }
          />
        </div>
        <aside className="min-w-0">
          <ReportsSide overview={overview.data} failures={failures} />
        </aside>
      </div>
    );
  }

  return (
    <Page toolbar={<Toolbar title={t('admin.reports.title')} pills={pills} />} head={head}>
      <div ref={measure}>{content}</div>
    </Page>
  );
}
