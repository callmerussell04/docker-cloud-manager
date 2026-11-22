import { useState } from 'react';
import { Plus, RefreshCcw } from 'lucide-react';

import { createDialogFlag, useSearchFlag } from '@/shared/lib/hooks';
import { Page } from '@/shared/ui/Page';
import { Toolbar, ToolbarButton, ToolbarPrimary } from '@/shared/ui/Toolbar';
import { Pagination } from '@/shared/ui/Pagination';
import { WarningBanner } from '@/shared/ui/WarningBanner';
import { ProjectWithContainersRow } from '@/features/projects/components/ProjectWithContainersRow';
import { CreateProjectModal } from '@/features/projects/components/CreateProjectModal';
import { useContainerModals } from '@/features/containers/components/useContainerModals';
import { projectTableLayout } from '../components/tableLayouts';
import { cn } from '@/shared/lib/utils';
import { useT } from '@/shared/i18n';
import { useProjects } from '@/features/projects/queries';
import { useBuildAvailability } from '@/features/images/queries';
import { SearchInput } from '@/shared/ui/SearchInput';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { EmptyState } from '@/shared/ui/EmptyState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { ErrorState } from '@/shared/ui/ErrorState';
import { failureReason } from '@/shared/api/apiError';

export function ProjectsPage() {
  const t = useT();
  const [isCreateModalOpen, setIsCreateModalOpen] = useSearchFlag(createDialogFlag);
  const { openLogs, openTerminal, openExpose, modals } = useContainerModals('own');
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const limit = 20;

  const { data, isLoading, isError, error, refetch, isFetching } = useProjects('own', page, limit);
  const projects = data?.items || [];

  const { data: buildAvailability } = useBuildAvailability();
  const isBuildDisabled = buildAvailability?.enabled === false;

  const filteredProjects = projects.filter(p => 
    p.name.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('projects.title')}
          count={data?.total_count}
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => refetch()} />}
          primary={<ToolbarPrimary icon={Plus} onClick={() => setIsCreateModalOpen(true)} label={t('projects.deploy')} />}
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

      {isBuildDisabled && (
        <WarningBanner className="shrink-0">
          {t('projects.composeBuildUnavailable')}
        </WarningBanner>
      )}

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col">
        <div className="overflow-x-auto flex-1">
          <div className={projectTableLayout.minWidth}>
            <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400", projectTableLayout.grid)}>
              <div className="flex items-center gap-3"><div className="w-10 shrink-0" /><div>{t('projects.projectName')}</div></div>
              <div>{t('common.status')}</div>
              <div>{t('status.error')}</div>
              <div>{t('projects.createdAt')}</div>
              <div className="flex justify-end">{t('common.actions')}</div>
            </div>

            <div className="flex flex-col">
              {isLoading ? (
                <LoadingRows columns={[1.2, 0.6, 1, 0.8, 0.4]} />
              ) : isError ? (
                <ErrorState
                    title={t('projects.loadFailed')}
                    message={failureReason(error, t)}
                    onRetry={() => refetch()}
                    isRetrying={isFetching}
                  />
              ) : filteredProjects.length > 0 ? (
                filteredProjects.map((project) => (
                  <ProjectWithContainersRow
                    key={project.id}
                    project={project}
                    onExpose={openExpose}
                    onViewLogs={openLogs}
                    onOpenTerminal={openTerminal}
                  />
                ))
              ) : search && projects.length > 0 ? (
                <NoMatchesState query={search} onClear={() => setSearch('')} />
              ) : (
                <EmptyState
                  title={t('projects.emptyTitle')}
                  description={t('projects.emptyDescription')}
                  action={{ label: t('projects.deploy'), icon: Plus, onClick: () => setIsCreateModalOpen(true) }}
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

      <CreateProjectModal 
        isOpen={isCreateModalOpen} 
        onClose={() => setIsCreateModalOpen(false)} 
        buildAvailability={buildAvailability}
      />

      {modals}
    </Page>
  );
}
