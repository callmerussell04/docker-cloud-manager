import { useMemo } from 'react';
import { Link } from 'react-router-dom';
import { ChevronRight, Disc, Layers, type LucideIcon } from 'lucide-react';
import { useT, type TFunction } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { useSizeClass } from '@/shared/lib/useSizeClass';
import { getServerErrorMessage } from '@/shared/api/apiError';
import { ErrorState } from '@/shared/ui/ErrorState';
import { Page } from '@/shared/ui/Page';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Toolbar } from '@/shared/ui/Toolbar';
import { useContainers } from '@/features/containers/queries';
import {
  containerNeedsPolling,
  containerPagePath,
  containerStatusLabel,
  containerTone,
  holdsMemoryReservation,
  type ContainerData,
} from '@/features/containers/model';
import { useProjects } from '@/features/projects/queries';
import { useDashboardStats } from '../queries';
import { memoryMapParts, projectSeries, type DashboardStats } from '../model';
import { MemoryMap, MemoryMapHead, type LegendEntry, type MapHolder } from '../components/MemoryMap';
import { AttentionColumn, type AttentionItem } from '../components/AttentionColumn';
import { QuotaBar } from '../components/QuotaBar';
import { NewMenu } from '../components/NewMenu';

// The dashboard reads the first page of the containers list, as the list
// itself does, and one page of projects for their names.
const pageSize = 100;
const containersPath = '/containers';

const megabytes = (value: number) => formatBytes(value * 1024 * 1024, 1);

function attentionItem(t: TFunction, container: ContainerData): AttentionItem {
  const tone = containerTone(container.status);
  const failure = tone === 'bad' ? getServerErrorMessage(container.last_error, t) : undefined;
  return {
    id: container.id,
    name: container.name,
    tone,
    detail: failure ?? containerStatusLabel(t, container.status),
    to: containerPagePath(containersPath, container.id),
  };
}

function CountLink({ to, icon: Icon, label }: { to: string; icon: LucideIcon; label: string }) {
  return (
    <Link to={to} className="inline-flex items-center gap-1.5 font-[550] text-accent hover:underline">
      <Icon aria-hidden className="h-[15px] w-[15px]" strokeWidth={2} />
      {label}
      <ChevronRight aria-hidden className="h-3.5 w-3.5" strokeWidth={2.2} />
    </Link>
  );
}

function Quotas({ stats }: { stats: DashboardStats }) {
  const t = useT();
  return (
    <div className="grid gap-x-7 gap-y-4 [grid-template-columns:repeat(auto-fit,minmax(min(100%,240px),1fr))]">
      <QuotaBar label={t('dashboard.quota.disk')} used={stats.disk_used_mb} total={stats.disk_quota_mb} format={megabytes} />
      <QuotaBar
        label={t('dashboard.quota.containers', { running: stats.containers_running })}
        used={stats.containers_total}
        total={stats.containers_quota}
      />
      <QuotaBar label={t('dashboard.quota.volumes')} used={stats.volumes_total} total={stats.volumes_quota} />
      <div className="col-span-full flex flex-wrap gap-x-5 gap-y-2">
        <CountLink to="/images" icon={Disc} label={t('dashboard.images', { count: stats.images_total })} />
        <CountLink to="/projects" icon={Layers} label={t('dashboard.projects', { count: stats.projects_total })} />
      </div>
    </div>
  );
}

function LoadingDashboard() {
  return (
    <div aria-hidden className="grid gap-5">
      <div className="h-11 w-72 max-w-full animate-[status-pulse_1.4s_ease-in-out_infinite] rounded-[12px] bg-fill" />
      <div className="h-[min(52vh,440px)] min-h-[240px] animate-[status-pulse_1.4s_ease-in-out_infinite] rounded-[18px] bg-fill" />
    </div>
  );
}

// DashboardPage leads with where the user's memory goes: a map of the memory
// quota with a block per container that holds memory, beside a column of the
// containers that need the user. Below the map are the other quotas and links
// to images and Compose projects; each number appears once. New in the
// toolbar opens the four create actions.
export function DashboardPage() {
  const t = useT();
  const sizeClass = useSizeClass();
  const { rowsCompact } = useShellLayout();
  const wide = sizeClass === 'wide' && !rowsCompact;
  const containers = useContainers('own', 1, pageSize);
  const items = containers.data?.items;
  // The figures follow the list while it polls, so the map's total and its
  // blocks change together.
  const polling = !!items?.some(containerNeedsPolling);
  const stats = useDashboardStats(true, polling);
  const projects = useProjects('own', 1, pageSize, { polling: false });

  const view = useMemo(() => {
    const list = items ?? [];
    const names = new Map(projects.data?.items.map((project) => [project.id, project.name]));
    const series = projectSeries([
      ...(projects.data?.items.map((project) => project.id) ?? []),
      ...list.flatMap((container) => (container.project_id ? [container.project_id] : [])),
    ]);
    const holding = list.filter(holdsMemoryReservation);
    const holders: MapHolder[] = holding.map((container) => ({
      id: container.id,
      bytes: container.memory_reservation_bytes,
      name: container.name,
      to: containerPagePath(containersPath, container.id),
      series: container.project_id ? series.get(container.project_id) : undefined,
    }));
    const legend: LegendEntry[] = [];
    const seen = new Set<string>();
    for (const container of holding) {
      const key = container.project_id ?? '';
      if (seen.has(key) || container.memory_reservation_bytes <= 0) continue;
      seen.add(key);
      legend.push(
        container.project_id
          ? { key, label: names.get(container.project_id) ?? t('containers.composeTag'), series: series.get(container.project_id) }
          : { key, label: t('containers.standalone') },
      );
    }
    // Projects in series order, then Standalone.
    legend.sort((a, b) => (a.series ?? Infinity) - (b.series ?? Infinity) || a.label.localeCompare(b.label));
    return {
      holders,
      legend,
      failing: list.filter((container) => containerTone(container.status) === 'bad').map((container) => attentionItem(t, container)),
      changing: list.filter((container) => containerTone(container.status) === 'busy').map((container) => attentionItem(t, container)),
    };
  }, [items, projects.data, t]);

  const parts = useMemo(
    () => (stats.data ? memoryMapParts(view.holders, stats.data.ram_used_bytes, stats.data.ram_quota_bytes) : []),
    [view.holders, stats.data],
  );

  const failed = stats.isError ? stats : containers.isError ? containers : undefined;
  const retry = () => {
    if (stats.isError) stats.refetch();
    if (containers.isError) containers.refetch();
  };

  let content;
  if (failed) {
    content = (
      <ErrorState
        title={t('dashboard.loadFailed')}
        error={failed.error}
        onRetry={retry}
        isRetrying={stats.isFetching || containers.isFetching}
      />
    );
  } else if (!stats.data || !items) {
    content = <LoadingDashboard />;
  } else {
    const head = (
      <MemoryMapHead
        usedBytes={stats.data.ram_used_bytes}
        quotaBytes={stats.data.ram_quota_bytes}
        legend={view.legend}
        other={parts.some((part) => part.kind === 'other')}
      />
    );
    const map = <MemoryMap parts={parts} />;
    const attention = <AttentionColumn failing={view.failing} changing={view.changing} />;
    const quotas = <Quotas stats={stats.data} />;
    content = wide ? (
      <div className="grid grid-cols-[minmax(0,1fr)_300px] items-start gap-x-7 gap-y-5">
        <div className="grid min-w-0 gap-5">
          {head}
          {map}
          {quotas}
        </div>
        <aside aria-label={t('dashboard.attention.label')} className="pt-1">{attention}</aside>
      </div>
    ) : (
      <div className="grid gap-6">
        <div className="grid gap-4">
          {head}
          {map}
        </div>
        <aside aria-label={t('dashboard.attention.label')}>{attention}</aside>
        {quotas}
      </div>
    );
  }

  return (
    <Page toolbar={<Toolbar title={t('dashboard.title')} primary={<NewMenu />} />}>
      {content}
    </Page>
  );
}
