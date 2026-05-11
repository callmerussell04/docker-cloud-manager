import { useState } from 'react';
import { Plus, RefreshCcw, Layers, Hammer } from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { Pagination } from '@/components/ui/Pagination';
import { WarningBanner } from '@/components/ui/WarningBanner';
import { ImageRow } from '@/features/images/components/ImageRow';
import { BuildRow } from '@/features/images/components/BuildRow';
import { CreateBuildModal } from '@/features/images/components/CreateBuildModal';
import { BuildLogsModal } from '@/features/images/components/BuildLogsModal';
import { type BuildData } from '@/features/images/types';
import { cn } from '@/lib/utils';
import { tableLayouts } from '@/components/ui/tableLayouts';
import { useT } from '@/lib/i18n';
import { useBuildAvailability, useBuilds, useImages } from '@/features/images/hooks';
import { SearchInput } from '@/components/common/SearchInput';
import { TabSwitcher } from '@/components/common/TabSwitcher';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';
import { getApiErrorMessage } from '@/lib/apiError';

type Tab = 'images' | 'builds';

export function ImagesPage() {
  const t = useT();
  const [activeTab, setActiveTab] = useState<Tab>('images');
  const [search, setSearch] = useState('');
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [viewLogsBuild, setViewLogsBuild] = useState<BuildData | null>(null);
  const [imagesPage, setImagesPage] = useState(1);
  const [buildsPage, setBuildsPage] = useState(1);
  const limit = 20;

  const { data: imagesData, isLoading: isLoadingImages, isError: isImagesError, error: imagesError, refetch: refetchImages, isFetching: isFetchingImages } = useImages(imagesPage, limit);
  const images = imagesData?.items || [];

  const { data: buildsData, isLoading: isLoadingBuilds, isError: isBuildsError, error: buildsError, refetch: refetchBuilds, isFetching: isFetchingBuilds } = useBuilds(buildsPage, limit, activeTab === 'builds');
  const builds = buildsData?.items || [];

  const { data: buildAvailability } = useBuildAvailability();
  const isBuildDisabled = buildAvailability?.enabled === false;

  const filteredImages = images.filter(i => i.tag.toLowerCase().includes(search.toLowerCase()));
  const filteredBuilds = builds.filter(b => b.id.toLowerCase().includes(search.toLowerCase()));

  const totalSizeMB = images.reduce((sum, img) => sum + img.size_mb, 0);

  const handleTabChange = (tab: Tab) => {
    setActiveTab(tab);
    setSearch('');
    if (tab === 'images') setImagesPage(1);
    else setBuildsPage(1);
  };

  const handleRefresh = () => {
    if (activeTab === 'images') refetchImages();
    else refetchBuilds();
  };

  const isFetching = activeTab === 'images' ? isFetchingImages : isFetchingBuilds;
  const isLoading = activeTab === 'images' ? isLoadingImages : isLoadingBuilds;

  return (
    <div className="space-y-6 flex flex-col h-full">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 shrink-0">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">{t('images.title')}</h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">
            {t('images.pageSize', { size: totalSizeMB })}
          </p>
        </div>
        
        <div className="flex items-center gap-3 w-full sm:w-auto">
          <SearchInput
            placeholder={t('common.search')}
            title={t('common.searchCurrentPage')}
            value={search}
            onChange={(value) => {
              setSearch(value);
              setImagesPage(1);
              setBuildsPage(1);
            }}
          />
          <Button variant="secondary" onClick={handleRefresh} isLoading={isFetching} className="px-3">
            <RefreshCcw className="w-4 h-4" />
          </Button>
          <Button onClick={() => setIsCreateModalOpen(true)} disabled={isBuildDisabled}>
            <Plus className="w-4 h-4 mr-2" />
            {t('images.build')}
          </Button>
        </div>
      </div>

      {isBuildDisabled && (
        <WarningBanner className="shrink-0">
          {t('images.buildUnavailable')}
        </WarningBanner>
      )}

      <TabSwitcher
        activeTab={activeTab}
        onChange={handleTabChange}
        items={[
          { id: 'images', label: t('images.myImages'), icon: Layers },
          { id: 'builds', label: t('images.buildHistory'), icon: Hammer },
        ]}
      />

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col">
        <div className="overflow-x-auto flex-1">
          <div className={activeTab === 'images' ? tableLayouts.images.minWidth : tableLayouts.builds.minWidth}>
            {activeTab === 'images' ? (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400", tableLayouts.images.grid)}>
                <div className="flex items-center gap-3"><div className="w-10 shrink-0" /><div>{t('images.tag')}</div></div>
                <div>{t('images.size')}</div>
                <div>{t('common.status')}</div>
                <div>{t('images.createdAt')}</div>
                <div className="flex justify-end">{t('common.actions')}</div>
              </div>
            ) : (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400", tableLayouts.builds.grid)}>
                <div>ID</div>
                <div>{t('common.status')}</div>
                <div>{t('images.duration')}</div>
                <div>{t('images.startedAt')}</div>
                <div className="flex justify-end">{t('common.actions')}</div>
              </div>
            )}

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
              ) : activeTab === 'images' && isImagesError ? (
                <div className="p-4">
                  <ErrorState title={t('images.loadFailed')} message={getApiErrorMessage(imagesError, t('images.loadFailed'), t).message} onRetry={() => refetchImages()} isRetrying={isFetchingImages} />
                </div>
              ) : activeTab === 'builds' && isBuildsError ? (
                <div className="p-4">
                  <ErrorState title={t('images.loadBuildsFailed')} message={getApiErrorMessage(buildsError, t('images.loadBuildsFailed'), t).message} onRetry={() => refetchBuilds()} isRetrying={isFetchingBuilds} />
                </div>
              ) : activeTab === 'images' ? (
                filteredImages.length > 0 ? (
                  filteredImages.map((image) => (
                    <ImageRow key={image.id} image={image} />
                  ))
                ) : (
                  <EmptyState
                    icon={<Layers className="h-8 w-8" />}
                    title={t('images.noImages')}
                    description={t('images.noImagesDescription')}
                    action={<Button onClick={() => setIsCreateModalOpen(true)} disabled={isBuildDisabled}>{t('images.buildFirst')}</Button>}
                  />
                )
              ) : (
                filteredBuilds.length > 0 ? (
                  filteredBuilds.map((build) => (
                    <BuildRow key={build.id} build={build} onViewLogs={setViewLogsBuild} />
                  ))
                ) : (
                  <EmptyState
                    icon={<Hammer className="h-8 w-8" />}
                    title={t('images.emptyBuilds')}
                    description={t('images.emptyBuildsDescription')}
                  />
                )
              )}
            </div>
          </div>
        </div>
        {activeTab === 'images' && imagesData && (
          <div className="shrink-0 bg-slate-50/50 dark:bg-slate-800/50">
            <Pagination currentPage={imagesPage} pageSize={limit} totalItems={imagesData.total_count} onPageChange={setImagesPage} />
          </div>
        )}
        {activeTab === 'builds' && buildsData && (
          <div className="shrink-0 bg-slate-50/50 dark:bg-slate-800/50">
            <Pagination currentPage={buildsPage} pageSize={limit} totalItems={buildsData.total_count} onPageChange={setBuildsPage} />
          </div>
        )}
      </div>

      <CreateBuildModal 
        isOpen={isCreateModalOpen} 
        onClose={() => setIsCreateModalOpen(false)}
        onSuccessSwitchTab={() => setActiveTab('builds')}
      />

      <BuildLogsModal 
        build={viewLogsBuild}
        onClose={() => setViewLogsBuild(null)}
      />
    </div>
  );
}
