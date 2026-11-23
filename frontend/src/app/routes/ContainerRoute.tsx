import { useCallback, useMemo } from 'react';
import type { Scope } from '@/shared/api/scope';
import type { TranslationKey } from '@/shared/i18n';
import { ContainerDetailsPage } from '@/features/containers/pages/ContainerDetailsPage';
import { useProjects } from '@/features/projects/queries';

interface ContainerRouteProps {
  scope: Scope;
  backTo: string;
  backLabel: TranslationKey;
}

// ContainerRoute gives a container's page the names of the Compose projects
// in its scope, for its Project detail. The containers feature may not read
// projects (projects reads containers), so the app layer joins them. One page
// of 100 projects covers any usual account; a project beyond it is named by
// the generic Compose tag.
export function ContainerRoute({ scope, backTo, backLabel }: ContainerRouteProps) {
  const { data } = useProjects(scope, 1, 100, { polling: false });
  const names = useMemo(() => new Map(data?.items.map((project) => [project.id, project.name])), [data]);
  const projectName = useCallback((projectId: string) => names.get(projectId), [names]);
  return <ContainerDetailsPage scope={scope} backTo={backTo} backLabel={backLabel} projectName={projectName} />;
}
