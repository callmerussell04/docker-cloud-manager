import { useState } from 'react';
import { Plus, RefreshCcw } from 'lucide-react';

import { Page } from '@/shared/ui/Page';
import { Toolbar, ToolbarButton, ToolbarPrimary } from '@/shared/ui/Toolbar';
import { Pagination } from '@/shared/ui/Pagination';
import { VolumeRow } from '@/features/volumes/components/VolumeRow';
import { CreateVolumeModal } from '@/features/volumes/components/CreateVolumeModal';
import { volumeTableLayout } from '../components/tableLayouts';
import { cn } from '@/shared/lib/utils';
import { useT } from '@/shared/i18n';
import { useVolumes } from '@/features/volumes/queries';
import { SearchInput } from '@/shared/ui/SearchInput';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { EmptyState } from '@/shared/ui/EmptyState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { ErrorState } from '@/shared/ui/ErrorState';
import { failureReason } from '@/shared/api/apiError';

export function VolumesPage() {
  const t = useT();
  const[isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const limit = 20;

  const { data, isLoading, isError, error, refetch, isFetching } = useVolumes('own', page, limit);
  const volumes = data?.items || [];

  const filteredVolumes = volumes.filter(v => 
    (v.name || '').toLowerCase().includes(search.toLowerCase()) || v.id.toLowerCase().includes(search.toLowerCase()) || v.status.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('volumes.title')}
          count={data?.total_count}
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => refetch()} />}
          primary={<ToolbarPrimary icon={Plus} onClick={() => setIsCreateModalOpen(true)} label={t('volumes.create')} />}
        />
      }
    >
      <div className="flex justify-end">
        <SearchInput
          placeholder={t('common.search')}
          title={t('common.searchCurrentPage')}
          value={search}
          onChange={setSearch}
        />
      </div>

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col">
        <div className="overflow-x-auto flex-1">
          <div className={volumeTableLayout.minWidth}>
            <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400", volumeTableLayout.grid)}>
              <div className="flex items-center gap-3"><div className="w-10 shrink-0" /><div>{t('volumes.name')}</div></div>
              <div>{t('common.status')}</div>
              <div>{t('volumes.usage')}</div>
              <div>{t('images.createdAt')}</div>
              <div className="flex justify-end">{t('common.actions')}</div>
            </div>

            <div className="flex flex-col">
              {isLoading ? (
                <LoadingRows columns={[1.5, 0.6, 0.8, 0.8, 0.3]} />
              ) : isError ? (
                <ErrorState
                    title={t('volumes.loadFailed')}
                    message={failureReason(error, t)}
                    onRetry={() => refetch()}
                    isRetrying={isFetching}
                  />
              ) : filteredVolumes.length > 0 ? (
                filteredVolumes.map((volume) => (
                  <VolumeRow key={volume.id} volume={volume} />
                ))
              ) : search && volumes.length > 0 ? (
                <NoMatchesState query={search} onClear={() => setSearch('')} />
              ) : (
                <EmptyState
                  title={t('volumes.emptyTitle')}
                  description={t('volumes.emptyDescription')}
                  action={{ label: t('volumes.create'), icon: Plus, onClick: () => setIsCreateModalOpen(true) }}
                />
              )}
            </div>
          </div>
        </div>
        {data && (
          <div className="shrink-0 bg-slate-50/50 dark:bg-slate-800/50">
            <Pagination currentPage={page} pageSize={limit} totalItems={data.total_count} onPageChange={setPage} />
          </div>
        )}
      </div>

      <CreateVolumeModal 
        isOpen={isCreateModalOpen} 
        onClose={() => setIsCreateModalOpen(false)} 
      />
    </Page>
  );
}
