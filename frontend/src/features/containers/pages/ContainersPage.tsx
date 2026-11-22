import { useState } from 'react';
import { Plus, RefreshCcw } from 'lucide-react';

import { createDialogFlag, useSearchFlag } from '@/shared/lib/hooks';
import { Page } from '@/shared/ui/Page';
import { Toolbar, ToolbarButton, ToolbarPrimary } from '@/shared/ui/Toolbar';
import { Pagination } from '@/shared/ui/Pagination';
import { ContainerRow } from '@/features/containers/components/ContainerRow';
import { CreateContainerModal } from '@/features/containers/components/CreateContainerModal';
import { useContainerModals } from '@/features/containers/components/useContainerModals';
import { containerTableLayout } from '../components/tableLayouts';
import { cn } from '@/shared/lib/utils';
import { useT } from '@/shared/i18n';
import { useContainers } from '@/features/containers/queries';
import { SearchInput } from '@/shared/ui/SearchInput';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { EmptyState } from '@/shared/ui/EmptyState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { ErrorState } from '@/shared/ui/ErrorState';
import { failureReason } from '@/shared/api/apiError';

export function ContainersPage() {
  const t = useT();
  const [isCreateModalOpen, setIsCreateModalOpen] = useSearchFlag(createDialogFlag);
  const { openLogs, openTerminal, openExpose, modals } = useContainerModals('own');
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const limit = 20;

  const { data, isLoading, isError, error, refetch, isFetching } = useContainers('own', page, limit);
  const containers = data?.items || [];

  const filteredContainers = containers.filter(c => 
    c.name.toLowerCase().includes(search.toLowerCase()) || 
    c.image_tag.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('containers.title')}
          count={data?.total_count}
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => refetch()} />}
          primary={<ToolbarPrimary icon={Plus} onClick={() => setIsCreateModalOpen(true)} label={t('containers.create')} />}
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
          <div className={containerTableLayout.minWidth}>
            <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400", containerTableLayout.grid)}>
              <div className="flex items-center gap-4"><div className="w-10 shrink-0" /><div>{t('containers.name')}</div></div>
              <div>{t('common.status')}</div>
              <div>{t('containers.routing')}</div>
              <div className="flex justify-end">{t('common.actions')}</div>
            </div>

            <div className="flex flex-col">
              {isLoading ? (
                <LoadingRows columns={[1.5, 0.8, 1, 0.6]} />
              ) : isError ? (
                <ErrorState
                    title={t('containers.loadFailed')}
                    message={failureReason(error, t)}
                    onRetry={() => refetch()}
                    isRetrying={isFetching}
                  />
              ) : filteredContainers.length > 0 ? (
                filteredContainers.map((container) => (
                  <ContainerRow 
                    key={container.id} 
                    container={container} 
                    onExpose={openExpose}
                    onViewLogs={openLogs}
                    onOpenTerminal={openTerminal}
                  />
                ))
              ) : search && containers.length > 0 ? (
                <NoMatchesState query={search} onClear={() => setSearch('')} />
              ) : (
                <EmptyState
                  title={t('containers.emptyTitle')}
                  description={t('containers.emptyDescription')}
                  action={{ label: t('containers.create'), icon: Plus, onClick: () => setIsCreateModalOpen(true) }}
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

      <CreateContainerModal 
        isOpen={isCreateModalOpen} 
        onClose={() => setIsCreateModalOpen(false)} 
      />

      {modals}
    </Page>
  );
}
