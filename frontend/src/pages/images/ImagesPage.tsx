import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus, RefreshCcw, Search, Layers, Hammer } from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Pagination } from '@/components/ui/Pagination';
import { WarningBanner } from '@/components/ui/WarningBanner';
import { ImageRow } from '@/features/images/components/ImageRow';
import { BuildRow } from '@/features/images/components/BuildRow';
import { CreateBuildModal } from '@/features/images/components/CreateBuildModal';
import { BuildLogsModal } from '@/features/images/components/BuildLogsModal';
import { getImagesFn, getBuildsFn, getBuildAvailabilityFn } from '@/features/images/api';
import { type BuildData } from '@/features/images/types';
import { cn } from '@/lib/utils';
import { tableLayouts } from '@/components/ui/tableLayouts';
import { useT } from '@/lib/i18n';

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

  const { data: imagesData, isLoading: isLoadingImages, refetch: refetchImages, isFetching: isFetchingImages } = useQuery({
    queryKey: ['images', imagesPage],
    queryFn: () => getImagesFn(imagesPage, limit),
  });
  const images = imagesData?.items || [];

  const { data: buildsData, isLoading: isLoadingBuilds, refetch: refetchBuilds, isFetching: isFetchingBuilds } = useQuery({
    queryKey: ['builds', buildsPage],
    queryFn: () => getBuildsFn(buildsPage, limit),
    refetchInterval: activeTab === 'builds' ? 5000 : false,
  });
  const builds = buildsData?.items || [];

  const { data: buildAvailability } = useQuery({
    queryKey: ['imageBuildAvailability'],
    queryFn: getBuildAvailabilityFn,
  });
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
          <div className="relative w-full sm:w-64">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
            <Input 
              placeholder={t('common.search')}
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setImagesPage(1);
                setBuildsPage(1);
              }}
              className="pl-9"
            />
          </div>
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

      <div className="max-w-full overflow-x-auto pb-1 shrink-0">
        <div className="flex w-max bg-white/40 dark:bg-slate-900/40 backdrop-blur-md p-1 rounded-xl border border-white/50 dark:border-slate-700/50">
        <button
          onClick={() => handleTabChange('images')}
          className={cn(
            "flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all",
            activeTab === 'images' 
              ? "bg-white dark:bg-slate-800 shadow-sm text-indigo-600 dark:text-indigo-400" 
              : "text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-100"
          )}
        >
          <Layers className="w-4 h-4" />
          {t('images.myImages')}
        </button>
        <button
          onClick={() => handleTabChange('builds')}
          className={cn(
            "flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all",
            activeTab === 'builds' 
              ? "bg-white dark:bg-slate-800 shadow-sm text-indigo-600 dark:text-indigo-400" 
              : "text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-100"
          )}
        >
          <Hammer className="w-4 h-4" />
          {t('images.buildHistory')}
        </button>
        </div>
      </div>

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
              ) : activeTab === 'images' ? (
                filteredImages.length > 0 ? (
                  filteredImages.map((image) => (
                    <ImageRow key={image.id} image={image} />
                  ))
                ) : (
                  <div className="p-12 flex flex-col items-center justify-center text-center">
                    <div className="w-16 h-16 bg-indigo-100 dark:bg-indigo-900/30 text-indigo-600 dark:text-indigo-400 rounded-2xl flex items-center justify-center mb-4">
                      <Layers className="w-8 h-8" />
                    </div>
                    <h3 className="text-xl font-semibold mb-2">{t('images.noImages')}</h3>
                    <p className="text-slate-500 dark:text-slate-400 max-w-sm mb-6">
                      {t('images.noImagesDescription')}
                    </p>
                    <Button onClick={() => setIsCreateModalOpen(true)} disabled={isBuildDisabled}>{t('images.buildFirst')}</Button>
                  </div>
                )
              ) : (
                filteredBuilds.length > 0 ? (
                  filteredBuilds.map((build) => (
                    <BuildRow key={build.id} build={build} onViewLogs={setViewLogsBuild} />
                  ))
                ) : (
                  <div className="p-12 flex flex-col items-center justify-center text-center">
                    <div className="w-16 h-16 bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 rounded-2xl flex items-center justify-center mb-4">
                      <Hammer className="w-8 h-8" />
                    </div>
                    <h3 className="text-xl font-semibold mb-2">{t('images.emptyBuilds')}</h3>
                    <p className="text-slate-500 dark:text-slate-400 max-w-sm">
                      {t('images.emptyBuildsDescription')}
                    </p>
                  </div>
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
