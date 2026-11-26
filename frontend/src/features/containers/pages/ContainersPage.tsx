import { CheckCircle2, Plus, RefreshCcw } from 'lucide-react';
import { useNavigate, useSearchParams } from 'react-router-dom';

import { useT } from '@/shared/i18n';
import { createDialogFlag, useDebouncedValue, useListState } from '@/shared/lib/hooks';
import { serverViewChange, toggleSort, type ListStateSpec } from '@/shared/lib/listState';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorState } from '@/shared/ui/ErrorState';
import { List, ListHead, type ListColumn } from '@/shared/ui/List';
import { ListMessageBelowHead } from '@/shared/ui/ListMessage';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { Page } from '@/shared/ui/Page';
import { Pagination } from '@/shared/ui/Pagination';
import { Pills } from '@/shared/ui/Pills';
import { Toolbar, ToolbarButton, ToolbarPrimary, ToolbarSearch } from '@/shared/ui/Toolbar';
import { useRefreshFailure } from '@/shared/ui/useRefreshFailure';
import { ContainerRow } from '../components/ContainerRow';
import { CreateContainerDialog } from '../components/CreateContainerDialog';
import { containerListLayout } from '../components/listLayout';
import type { MemoryQuota } from '../createFlow';
import { useExposeDialog } from '../components/useExposeDialog';
import {
  containerFilters,
  containerListParams,
  containerSortKeys,
  containerPagePath,
  createWithImageParam,
  defaultContainerSort,
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

// The server filters, sorts, searches and counts the whole list; a page
// holds 100 of what they leave.
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
  const { openExpose, exposeDialog } = useExposeDialog('own');
  const [view, setView] = useListState(listSpec);
  const changeView = (change: Parameters<typeof setView>[0]) => setView(serverViewChange(change));
  // The URL follows each keystroke; the server is asked once typing pauses.
  const search = useDebouncedValue(view.search);
  const list = containerListParams({ filter: view.filter, sort: view.sort, search });

  const query = useContainers('own', view.page, pageSize, { list });
  const { data, isLoading, isError, error, refetch, isFetching } = query;
  const stale = useRefreshFailure(query, t('common.refreshFailed'));
  const shown = data?.items ?? [];
  const counts = data?.status_counts;

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

  const shape = { template: containerListLayout.wide, compactTemplate: containerListLayout.compact };
  const loading = <LoadingRows {...shape} />;
  const content = () => {
    if (isLoading) return loading;
    if (isError && !data) {
      return <ErrorState {...shape} title={t('containers.loadFailed')} error={error} onRetry={() => refetch()} isRetrying={isFetching} />;
    }
    if (counts?.all === 0 && !list.q) {
      return (
        <EmptyState
          {...shape}
          title={t('containers.emptyTitle')}
          description={t('containers.emptyDescription')}
          action={{ label: t('containers.create'), icon: Plus, onClick: () => setIsCreateOpen(true) }}
        />
      );
    }
    if (counts?.all === 0) {
      return <NoMatchesState query={view.search} onClear={() => changeView({ search: '' })} />;
    }
    // A page past the end returns to the last page (Pagination) and loads.
    if (shown.length === 0 && data && data.total_count > 0) return loading;
    if (shown.length === 0) {
      // Attention showing nothing is good news; another filter, only empty.
      const attention = view.filter === 'attention';
      return (
        <ListMessageBelowHead
          role="status"
          icon={attention ? <CheckCircle2 aria-hidden className="mb-1 h-7 w-7 text-green" strokeWidth={1.8} /> : undefined}
          title={t(attention ? 'containers.attentionEmpty' : 'containers.filterEmpty')}
          description={t(attention ? 'containers.attentionEmptyDescription' : 'containers.filterEmptyDescription')}
          action={{ label: t('containers.showAll'), onClick: () => changeView({ filter: 'all' }) }}
        />
      );
    }
    return (
      <List label={t('containers.title')} stale={stale}>
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
          count={counts?.all}
          pills={
            <div className="flex items-center gap-2">
              <Pills
                label={t('containers.filterLabel')}
                value={view.filter}
                onChange={(filter) => changeView({ filter })}
                items={containerFilters.map((filter) => ({ id: filter, label: t(`containers.filter.${filter}`), count: counts?.[filter] }))}
              />
              <ToolbarSearch label={t('containers.searchLabel')} value={view.search} onChange={(search) => changeView({ search })} />
            </div>
          }
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => refetch()} />}
          primary={<ToolbarPrimary icon={Plus} onClick={() => setIsCreateOpen(true)} label={t('containers.create')} />}
        />
      }
      head={
        <ListHead
          columns={columns}
          template={containerListLayout.wide}
          sort={view.sort}
          onSort={(key) => changeView({ sort: toggleSort(view.sort, key) })}
        />
      }
      className="space-y-2"
    >
      {content()}
      {data && <Pagination currentPage={view.page} pageSize={pageSize} totalItems={data.total_count} onPageChange={(page) => changeView({ page })} />}

      <CreateContainerDialog isOpen={isCreateOpen} onClose={() => setIsCreateOpen(false)} memory={memory} initialImage={initialImage} />

      {exposeDialog}
    </Page>
  );
}
