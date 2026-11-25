import { Plus, RefreshCcw } from 'lucide-react';
import { useNavigate, useSearchParams } from 'react-router-dom';

import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { createDialogFlag, createWithImageParam, useListState } from '@/shared/lib/hooks';
import { toggleSort, type ListStateSpec } from '@/shared/lib/listState';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorState } from '@/shared/ui/ErrorState';
import { List, ListHead, type ListColumn } from '@/shared/ui/List';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { Page } from '@/shared/ui/Page';
import { Pagination } from '@/shared/ui/Pagination';
import { Pills } from '@/shared/ui/Pills';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { StateMessage } from '@/shared/ui/StateMessage';
import { Toolbar, ToolbarButton, ToolbarPrimary, ToolbarSearch } from '@/shared/ui/Toolbar';
import { ContainerRow } from '../components/ContainerRow';
import { CreateContainerDialog } from '../components/CreateContainerDialog';
import { containerListLayout } from '../components/listLayout';
import type { MemoryQuota } from '../createFlow';
import { useContainerModals } from '../components/useContainerModals';
import {
  containerFilterCounts,
  containerFilters,
  containerSortKeys,
  containerPagePath,
  defaultContainerSort,
  filterContainers,
  matchesContainerSearch,
  sortContainers,
  type ConsoleView,
  type ContainerData,
  type ContainerFilter,
  type ContainerSortKey,
} from '../model';
import { useContainers } from '../queries';

const listSpec: ListStateSpec<ContainerFilter, ContainerSortKey> = {
  filters: containerFilters,
  sortKeys: containerSortKeys,
  defaultSort: defaultContainerSort,
};

// One page holds a user's whole list as the quota usually allows it; the
// filter, its counts, the sort and the search apply to the page shown.
const pageSize = 100;

interface ContainersPageProps {
  // projectName names a Compose project by its ID, for the tag beside its
  // containers; undefined while the projects are not known.
  projectName?: (projectId: string) => string | undefined;
  // memory is the user's memory quota and what they reserve, for the create
  // dialog's quota bar; undefined while it is not known.
  memory?: MemoryQuota;
}

export function ContainersPage({ projectName, memory }: ContainersPageProps) {
  const t = useT();
  const navigate = useNavigate();
  const compact = useShellLayout().rowsCompact;
  // ?new opens the create dialog, and ?image (from an image's Run a
  // container) is the image it starts with; closing it drops both.
  const [params, setParams] = useSearchParams();
  const isCreateOpen = params.has(createDialogFlag);
  const initialImage = params.get(createWithImageParam) ?? undefined;
  const setIsCreateOpen = (open: boolean) => setParams((current) => {
    const updated = new URLSearchParams(current);
    if (open) updated.set(createDialogFlag, '');
    else {
      updated.delete(createDialogFlag);
      updated.delete(createWithImageParam);
    }
    return updated;
  }, { replace: true });
  const { openExpose, modals } = useContainerModals('own');
  const [view, changeView] = useListState(listSpec);

  const { data, isLoading, isError, error, refetch, isFetching } = useContainers('own', view.page, pageSize);
  const containers = data?.items ?? [];
  const searched = containers.filter((container) => matchesContainerSearch(container, view.search));
  const counts = containerFilterCounts(searched);
  const shown = sortContainers(filterContainers(searched, view.filter), view.sort);

  const columns: ListColumn<ContainerSortKey>[] = [
    { key: 'name', label: t('containers.name') },
    { key: 'status', label: t('common.status') },
    { key: 'address', label: t('containers.column.address') },
    { key: 'memory', label: t('containers.column.memory') },
    { key: 'created', label: t('containers.column.created'), end: true },
  ];

  const openContainer = (container: ContainerData, view?: ConsoleView) =>
    navigate(containerPagePath('/containers', container.id, view), { state: { container } });
  const projectOf = (container: ContainerData) =>
    container.project_id ? (projectName?.(container.project_id) ?? t('containers.composeTag')) : undefined;

  const content = () => {
    if (isLoading) return <LoadingRows columns={compact ? [1] : [1.5, 1.2, 1, 0.4, 0.5]} />;
    if (isError) {
      return <ErrorState title={t('containers.loadFailed')} message={failureReason(error, t)} onRetry={() => refetch()} isRetrying={isFetching} />;
    }
    if (containers.length === 0) {
      return (
        <EmptyState
          title={t('containers.emptyTitle')}
          description={t('containers.emptyDescription')}
          action={{ label: t('containers.create'), icon: Plus, onClick: () => setIsCreateOpen(true) }}
        />
      );
    }
    if (shown.length === 0 && searched.length === 0) {
      return <NoMatchesState query={view.search} onClear={() => changeView({ search: '' })} />;
    }
    if (shown.length === 0) {
      return (
        <StateMessage
          role="status"
          title={t('containers.filterEmpty')}
          description={t('containers.filterEmptyDescription')}
          action={{ label: t('containers.showAll'), onClick: () => changeView({ filter: 'all' }) }}
        />
      );
    }
    return (
      <List label={t('containers.title')}>
        {shown.map((container) => (
          <ContainerRow
            key={container.id}
            container={container}
            project={projectOf(container)}
            onOpen={openContainer}
            onExpose={openExpose}
          />
        ))}
      </List>
    );
  };

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('containers.title')}
          count={data?.total_count}
          pills={
            <div className="flex items-center gap-2">
              <Pills
                label={t('containers.filterLabel')}
                value={view.filter}
                onChange={(filter) => changeView({ filter })}
                items={containerFilters.map((filter) => ({ id: filter, label: t(`containers.filter.${filter}`), count: counts[filter] }))}
              />
              <ToolbarSearch label={t('containers.searchLabel')} value={view.search} onChange={(search) => changeView({ search })} />
            </div>
          }
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => refetch()} />}
          primary={<ToolbarPrimary icon={Plus} onClick={() => setIsCreateOpen(true)} label={t('containers.create')} />}
        />
      }
      head={(isLoading || shown.length > 0) && (
        <ListHead
          columns={columns}
          template={containerListLayout.wide}
          sort={view.sort}
          onSort={(key) => changeView({ sort: toggleSort(view.sort, key) })}
        />
      )}
      className="space-y-2"
    >
      {content()}
      {data && <Pagination currentPage={view.page} pageSize={pageSize} totalItems={data.total_count} onPageChange={(page) => changeView({ page })} />}

      <CreateContainerDialog isOpen={isCreateOpen} onClose={() => setIsCreateOpen(false)} memory={memory} initialImage={initialImage} />

      {modals}
    </Page>
  );
}
