import { useCallback, useMemo } from 'react';
import { ContainersPage } from '@/features/containers/pages/ContainersPage';
import { useProjects } from '@/features/projects/queries';

// ContainersRoute gives the containers list the names of the user's Compose
// projects for the tag beside their containers. The containers feature may not
// read projects (projects reads containers), so the app layer joins them.
// One page of 100 projects covers any usual account; a container whose
// project is beyond it gets the generic Compose tag.
export function ContainersRoute() {
  const { data } = useProjects('own', 1, 100, { polling: false });
  const names = useMemo(() => new Map(data?.items.map((project) => [project.id, project.name])), [data]);
  const projectName = useCallback((projectId: string) => names.get(projectId), [names]);
  return <ContainersPage projectName={projectName} />;
}
