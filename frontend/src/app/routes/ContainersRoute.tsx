import { useCallback, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ContainersPage } from '@/features/containers/pages/ContainersPage';
import { useDashboardStats } from '@/features/dashboard/queries';
import { useProjects } from '@/features/projects/queries';
import { createDialogFlag } from '@/shared/lib/hooks';

// ContainersRoute gives the containers list the names of the user's Compose
// projects for the tag beside their containers, and the create dialog the
// user's memory quota from the dashboard's figures. The containers feature may
// read neither projects (projects reads containers) nor the dashboard, so the
// app layer joins them. One page of 100 projects covers any usual account; a
// container whose project is beyond it gets the generic Compose tag.
export function ContainersRoute() {
  const { data } = useProjects('own', 1, 100, { polling: false });
  // The figures are read only while the create dialog is open.
  const [search] = useSearchParams();
  const { data: stats } = useDashboardStats(search.has(createDialogFlag));
  const names = useMemo(() => new Map(data?.items.map((project) => [project.id, project.name])), [data]);
  const projectName = useCallback((projectId: string) => names.get(projectId), [names]);
  const memory = useMemo(
    () => (stats ? { usedBytes: stats.ram_used_bytes, quotaBytes: stats.ram_quota_bytes } : undefined),
    [stats],
  );
  return <ContainersPage projectName={projectName} memory={memory} />;
}
