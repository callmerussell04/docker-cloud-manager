import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { buildSlotsInUse, buildsQueued, type SystemMonitoring } from '../model';

// HostCounts is the host's inventory: containers with how many run,
// problems (failed and missing, in red while there are any), volumes,
// images, projects and builds running, with how many wait for a slot.
export function HostCounts({ stats }: { stats: SystemMonitoring }) {
  const t = useT();
  const problems = stats.containers_error + stats.containers_missing;
  const items: { key: string; label: string; value: number; sub?: string; bad?: boolean }[] = [
    {
      key: 'containers',
      label: t('admin.monitoring.count.containers'),
      value: stats.containers_total,
      sub: t('admin.monitoring.count.containersDetail', { running: stats.containers_running }),
    },
    {
      key: 'problems',
      label: t('admin.monitoring.count.problems'),
      value: problems,
      sub: problems > 0 ? t('admin.monitoring.count.problemsDetail', { failed: stats.containers_error, missing: stats.containers_missing }) : undefined,
      bad: problems > 0,
    },
    { key: 'volumes', label: t('admin.monitoring.count.volumes'), value: stats.volumes_total },
    { key: 'images', label: t('admin.monitoring.count.images'), value: stats.images_total },
    { key: 'projects', label: t('admin.monitoring.count.projects'), value: stats.projects_total },
    {
      key: 'builds',
      label: t('admin.monitoring.count.builds'),
      value: buildSlotsInUse(stats),
      sub: buildsQueued(stats) > 0 ? t('admin.monitoring.count.buildsQueued', { queued: buildsQueued(stats) }) : undefined,
    },
  ];
  return (
    <dl className="grid gap-x-6 gap-y-4 [grid-template-columns:repeat(auto-fill,minmax(min(100%,130px),1fr))]">
      {items.map((item) => (
        <div key={item.key} className="min-w-0">
          <dt className={cn('truncate text-[12px] font-semibold', item.bad ? 'text-red-text' : 'text-label-2')}>{item.label}</dt>
          <dd className={cn('text-[24px] font-[650] tabular-nums tracking-[-0.02em]', item.bad ? 'text-red-text' : 'text-label')}>{item.value}</dd>
          {item.sub && <dd className={cn('text-[12px]', item.bad ? 'text-red-text' : 'text-label-2')}>{item.sub}</dd>}
        </div>
      ))}
    </dl>
  );
}
