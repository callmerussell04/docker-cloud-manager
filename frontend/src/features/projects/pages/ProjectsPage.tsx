import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { Plus, RefreshCcw } from 'lucide-react';

import { useT } from '@/shared/i18n';
import { createDialogFlag, useListState, useSearchFlag } from '@/shared/lib/hooks';
import { toggleSort, type ListStateSpec } from '@/shared/lib/listState';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorState } from '@/shared/ui/ErrorState';
import { List, ListHead, type ListColumn } from '@/shared/ui/List';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { Page } from '@/shared/ui/Page';
import { Pagination } from '@/shared/ui/Pagination';
import { useRefreshFailure } from '@/shared/ui/useRefreshFailure';
import { Toolbar, ToolbarButton, ToolbarPrimary, ToolbarSearch } from '@/shared/ui/Toolbar';
import { useContainers } from '@/features/containers/queries';
import { DeployProjectDialog } from '../components/DeployProjectDialog';
import { projectListLayout } from '../components/listLayout';
import { ProjectRow } from '../components/ProjectRow';
import {
  defaultProjectSort,
  isProjectBusy,
  matchesProjectSearch,
  projectSortKeys,
  servicesByProject,
  sortProjects,
  type ProjectSortKey,
} from '../model';
import { useProjects } from '../queries';

// The projects list has no filter; its one filter value keeps the URL state
// shape every list shares.
const listSpec: ListStateSpec<'all', ProjectSortKey> = {
  filters: ['all'],
  sortKeys: projectSortKeys,
  defaultSort: defaultProjectSort,
};

// One page holds a user's whole list as usual quotas allow it; the sort and
// the search apply to the page shown.
const pageSize = 100;

// ProjectsPage lists the user's Compose projects with their services, which
// come from the first 100 containers (the server's largest page): a service
// beyond it is not counted, which no usual account reaches.
export function ProjectsPage() {
  const t = useT();
  const navigate = useNavigate();
  const [isDeployOpen, setIsDeployOpen] = useSearchFlag(createDialogFlag);
  const [view, changeView] = useListState(listSpec);

  const query = useProjects('own', view.page, pageSize);
  const { data, isLoading, isError, error, refetch, isFetching } = query;
  const stale = useRefreshFailure(query, t('common.refreshFailed'));
  const shape = { template: projectListLayout.wide, compactTemplate: projectListLayout.compact };
  const projects = useMemo(() => data?.items ?? [], [data]);
  // A project that deploys, starts or stops changes its containers, so they
  // keep refreshing with it.
  const containers = useContainers('own', 1, 100, { polling: projects.some((project) => isProjectBusy(project.status)) });
  const services = useMemo(() => servicesByProject(containers.data?.items ?? []), [containers.data]);

  const items = projects.map((project) => ({ project, services: services.get(project.id) ?? [] }));
  const shown = sortProjects(items.filter((item) => matchesProjectSearch(item, view.search)), view.sort);

  const columns: ListColumn<ProjectSortKey>[] = [
    { key: 'name', label: t('projects.project') },
    { key: 'services', label: t('projects.services') },
    { key: 'address', label: t('projects.column.address') },
    { key: 'memory', label: t('projects.column.memory') },
    { key: 'created', label: t('projects.column.created'), end: true },
  ];

  const content = () => {
    if (isLoading) return <LoadingRows {...shape} />;
    if (isError && !data) {
      return <ErrorState {...shape} title={t('projects.loadFailed')} error={error} onRetry={() => refetch()} isRetrying={isFetching} />;
    }
    if (projects.length === 0) {
      return (
        <EmptyState
          {...shape}
          title={t('projects.emptyTitle')}
          description={t('projects.emptyDescription')}
          action={{ label: t('projects.deployTitle'), icon: Plus, onClick: () => setIsDeployOpen(true) }}
        />
      );
    }
    if (shown.length === 0) {
      return <NoMatchesState query={view.search} onClear={() => changeView({ search: '' })} />;
    }
    return (
      <List label={t('projects.title')} stale={stale}>
        {shown.map((item) => (
          <ProjectRow
            key={item.project.id}
            item={item}
            onOpen={() => navigate(`/projects/${item.project.id}`, { state: { project: item.project } })}
          />
        ))}
      </List>
    );
  };

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('projects.title')}
          count={data?.total_count}
          actions={
            <>
              <ToolbarSearch label={t('projects.searchLabel')} value={view.search} onChange={(search) => changeView({ search })} />
              <ToolbarButton
                icon={RefreshCcw}
                label={t('common.refresh')}
                busy={isFetching || containers.isFetching}
                onClick={() => {
                  void refetch();
                  void containers.refetch();
                }}
              />
            </>
          }
          primary={<ToolbarPrimary icon={Plus} onClick={() => setIsDeployOpen(true)} label={t('projects.deployTitle')} />}
        />
      }
      head={
        <ListHead
          columns={columns}
          template={projectListLayout.wide}
          sort={view.sort}
          onSort={(key) => changeView({ sort: toggleSort(view.sort, key) })}
        />
      }
      className="space-y-2"
    >
      {content()}
      {data && <Pagination currentPage={view.page} pageSize={pageSize} totalItems={data.total_count} onPageChange={(page) => changeView({ page })} />}

      <DeployProjectDialog isOpen={isDeployOpen} onClose={() => setIsDeployOpen(false)} />
    </Page>
  );
}
