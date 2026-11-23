import { Plus, RefreshCcw } from 'lucide-react';

import { failureReason } from '@/shared/api/apiError';
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
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Toolbar, ToolbarButton, ToolbarPrimary, ToolbarSearch } from '@/shared/ui/Toolbar';
import { CreateVolumeDialog } from '../components/CreateVolumeDialog';
import { volumeListLayout } from '../components/listLayout';
import { VolumeRow } from '../components/VolumeRow';
import type { DiskQuota } from '../createFlow';
import { defaultVolumeSort, matchesVolumeSearch, sortVolumes, volumeSortKeys, type VolumeSortKey } from '../model';
import { useVolumes } from '../queries';

// The volumes list has no filter; its one filter value keeps the URL state
// shape every list shares.
const listSpec: ListStateSpec<'all', VolumeSortKey> = {
  filters: ['all'],
  sortKeys: volumeSortKeys,
  defaultSort: defaultVolumeSort,
};

// One page holds a user's whole list as the volume quota usually allows it;
// the sort and the search apply to the page shown.
const pageSize = 100;

interface VolumesPageProps {
  // disk is the user's disk quota and what they reserve, for the create
  // dialog's quota bar; undefined while it is not known.
  disk?: DiskQuota;
}

export function VolumesPage({ disk }: VolumesPageProps) {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const [isCreateOpen, setIsCreateOpen] = useSearchFlag(createDialogFlag);
  const [view, changeView] = useListState(listSpec);

  const { data, isLoading, isError, error, refetch, isFetching } = useVolumes('own', view.page, pageSize);
  const volumes = data?.items ?? [];
  const shown = sortVolumes(volumes.filter((volume) => matchesVolumeSearch(volume, view.search)), view.sort);

  const columns: ListColumn<VolumeSortKey>[] = [
    { key: 'name', label: t('volumes.column.volume') },
    { key: 'status', label: t('common.status') },
    { key: 'size', label: t('volumes.column.size') },
    { key: 'usedBy', label: t('volumes.column.usedBy') },
    { key: 'created', label: t('volumes.column.created'), end: true },
  ];

  const content = () => {
    if (isLoading) return <LoadingRows columns={compact ? [1] : [1.3, 0.9, 0.9, 1, 0.5]} />;
    if (isError) {
      return <ErrorState title={t('volumes.loadFailed')} message={failureReason(error, t)} onRetry={() => refetch()} isRetrying={isFetching} />;
    }
    if (volumes.length === 0) {
      return (
        <EmptyState
          title={t('volumes.emptyTitle')}
          description={t('volumes.emptyDescription')}
          action={{ label: t('volumes.create'), icon: Plus, onClick: () => setIsCreateOpen(true) }}
        />
      );
    }
    if (shown.length === 0) {
      return <NoMatchesState query={view.search} onClear={() => changeView({ search: '' })} />;
    }
    return (
      <List label={t('volumes.title')}>
        {shown.map((volume) => <VolumeRow key={volume.id} volume={volume} />)}
      </List>
    );
  };

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('volumes.title')}
          count={data?.total_count}
          pills={<ToolbarSearch label={t('volumes.searchLabel')} value={view.search} onChange={(search) => changeView({ search })} />}
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => refetch()} />}
          primary={<ToolbarPrimary icon={Plus} onClick={() => setIsCreateOpen(true)} label={t('volumes.create')} />}
        />
      }
      head={(isLoading || shown.length > 0) && (
        <ListHead
          columns={columns}
          template={volumeListLayout.wide}
          sort={view.sort}
          onSort={(key) => changeView({ sort: toggleSort(view.sort, key) })}
        />
      )}
      className="space-y-2"
    >
      {content()}
      {data && <Pagination currentPage={view.page} pageSize={pageSize} totalItems={data.total_count} onPageChange={(page) => changeView({ page })} />}

      <CreateVolumeDialog isOpen={isCreateOpen} onClose={() => setIsCreateOpen(false)} disk={disk} />
    </Page>
  );
}
