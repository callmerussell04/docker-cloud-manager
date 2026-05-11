import { useState } from 'react';
import { Plus, RefreshCcw, Layers } from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { Pagination } from '@/components/ui/Pagination';
import { WarningBanner } from '@/components/ui/WarningBanner';
import { ProjectRow } from '@/features/projects/components/ProjectRow';
import { CreateProjectModal } from '@/features/projects/components/CreateProjectModal';
import { tableLayouts } from '@/components/ui/tableLayouts';
import { cn } from '@/lib/utils';
import { useT } from '@/lib/i18n';
import { useProjects } from '@/features/projects/hooks';
import { useBuildAvailability } from '@/features/images/hooks';
import { SearchInput } from '@/components/common/SearchInput';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { getApiErrorMessage } from '@/lib/apiError';

export function ProjectsPage() {
  const t = useT();
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const limit = 20;

  const { data, isLoading, isError, error, refetch, isFetching } = useProjects(page, limit);
  const projects = data?.items || [];

  const { data: buildAvailability } = useBuildAvailability();
  const isBuildDisabled = buildAvailability?.enabled === false;

  const filteredProjects = projects.filter(p => 
    p.name.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6 flex flex-col h-full">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 shrink-0">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">{t('projects.title')}</h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">{t('projects.subtitle')}</p>
        </div>
        
        <div className="flex items-center gap-3 w-full sm:w-auto">
          <SearchInput
            placeholder={t('common.search')}
            title={t('common.searchCurrentPage')}
            value={search}
            onChange={(value) => {
              setSearch(value);
              setPage(1);
            }}
          />
          <Button variant="secondary" onClick={() => refetch()} isLoading={isFetching} className="px-3">
            <RefreshCcw className="w-4 h-4" />
          </Button>
          <Button onClick={() => setIsCreateModalOpen(true)}>
            <Plus className="w-4 h-4 mr-2" />
            {t('projects.deploy')}
          </Button>
        </div>
      </div>

      {isBuildDisabled && (
        <WarningBanner className="shrink-0">
          {t('projects.composeBuildUnavailable')}
        </WarningBanner>
      )}

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col">
        <div className="overflow-x-auto flex-1">
          <div className={tableLayouts.projects.minWidth}>
            <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400", tableLayouts.projects.grid)}>
              <div className="flex items-center gap-3"><div className="w-10 shrink-0" /><div>{t('projects.projectName')}</div></div>
              <div>{t('common.status')}</div>
              <div>{t('status.error')}</div>
              <div>{t('projects.createdAt')}</div>
              <div className="flex justify-end">{t('common.actions')}</div>
            </div>

            <div className="flex flex-col">
              {isLoading ? (
                [...Array(5)].map((_, i) => (
                  <div key={i} className="flex gap-4 p-4 items-center border-b border-white/20 dark:border-slate-700/50">
                    <div className="w-10 h-10 rounded-xl bg-slate-200/50 dark:bg-slate-700/50 animate-pulse shrink-0" />
                    <div className="flex-1 space-y-2">
                      <div className="h-4 bg-slate-200/50 dark:bg-slate-700/50 rounded w-1/3 animate-pulse" />
                      <div className="h-3 bg-slate-200/50 dark:bg-slate-700/50 rounded w-1/2 animate-pulse" />
                    </div>
                  </div>
                ))
              ) : isError ? (
                <div className="p-4">
                  <ErrorState
                    title={t('projects.loadFailed')}
                    message={getApiErrorMessage(error, t('projects.loadFailed'), t).message}
                    onRetry={() => refetch()}
                    isRetrying={isFetching}
                  />
                </div>
              ) : filteredProjects.length > 0 ? (
                filteredProjects.map((project) => (
                  <ProjectRow key={project.id} project={project} />
                ))
              ) : (
                <EmptyState
                  icon={<Layers className="h-8 w-8" />}
                  title={t('projects.emptyTitle')}
                  description={t('projects.emptyDescription')}
                  action={<Button onClick={() => setIsCreateModalOpen(true)}>{t('projects.deploy')}</Button>}
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
    </div>
  );
}
