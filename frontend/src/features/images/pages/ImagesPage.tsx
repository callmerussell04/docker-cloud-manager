import { useEffect, useState } from 'react';
import { Plus, RefreshCcw, Layers, Hammer } from 'lucide-react';

import { Page } from '@/shared/ui/Page';
import { Toolbar, ToolbarButton, ToolbarPrimary } from '@/shared/ui/Toolbar';
import { Pagination } from '@/shared/ui/Pagination';
import { WarningBanner } from '@/shared/ui/WarningBanner';
import { ImageRow } from '@/features/images/components/ImageRow';
import { BuildRow } from '@/features/images/components/BuildRow';
import { CreateBuildModal } from '@/features/images/components/CreateBuildModal';
import { BuildLogsModal } from '@/features/images/components/BuildLogsModal';
import { type BuildData } from '@/features/images/model';
import { cn, formatBytes } from '@/shared/lib/utils';
import { imageTableLayout, buildTableLayout } from '../components/tableLayouts';
import { useT } from '@/shared/i18n';
import { createDialogFlag, useSearchFlag } from '@/shared/lib/hooks';
import { useBuildAvailability, useBuilds, useImages } from '@/features/images/queries';
import { SearchInput } from '@/shared/ui/SearchInput';
import { TabSwitcher } from '@/shared/ui/TabSwitcher';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { EmptyState } from '@/shared/ui/EmptyState';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { ErrorState } from '@/shared/ui/ErrorState';
import { failureReason } from '@/shared/api/apiError';

type Tab = 'images' | 'builds';

export function ImagesPage() {
  const t = useT();
  const [activeTab, setActiveTab] = useState<Tab>('images');
  const [search, setSearch] = useState('');
  const [isCreateModalOpen, setIsCreateModalOpen] = useSearchFlag(createDialogFlag);
  const [viewLogsBuild, setViewLogsBuild] = useState<BuildData | null>(null);
  const [imagesPage, setImagesPage] = useState(1);
  const [buildsPage, setBuildsPage] = useState(1);
  const limit = 20;

  const { data: imagesData, isLoading: isLoadingImages, isError: isImagesError, error: imagesError, refetch: refetchImages, isFetching: isFetchingImages } = useImages('own', imagesPage, limit);
  const images = imagesData?.items || [];

  const { data: buildsData, isLoading: isLoadingBuilds, isError: isBuildsError, error: buildsError, refetch: refetchBuilds, isFetching: isFetchingBuilds } = useBuilds('own', buildsPage, limit, { polling: activeTab === 'builds' });
  const builds = buildsData?.items || [];

  const { data: buildAvailability } = useBuildAvailability();
  const isBuildDisabled = buildAvailability?.enabled === false;
  // A link that asks for the build dialog (?new) while builds are off opens
  // nothing, so the flag is dropped.
  useEffect(() => {
    if (isCreateModalOpen && isBuildDisabled) setIsCreateModalOpen(false);
  }, [isCreateModalOpen, isBuildDisabled, setIsCreateModalOpen]);

  const filteredImages = images.filter(i => i.tag.toLowerCase().includes(search.toLowerCase()));
  const filteredBuilds = builds.filter(b => b.id.toLowerCase().includes(search.toLowerCase()) || (b.tag || '').toLowerCase().includes(search.toLowerCase()));

  const totalSizeBytes = images.reduce((sum, img) => sum + img.size_bytes, 0);

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
    <Page
      toolbar={
        <Toolbar
          title={t('images.title')}
          count={(activeTab === 'images' ? imagesData : buildsData)?.total_count}
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={handleRefresh} />}
          primary={<ToolbarPrimary icon={Plus} onClick={() => setIsCreateModalOpen(true)} disabled={isBuildDisabled} label={t('images.build')} />}
        />
      }
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-label-2">{t('images.pageSize', { size: formatBytes(totalSizeBytes) })}</p>
        <SearchInput
          placeholder={t('common.search')}
          title={t('common.searchCurrentPage')}
          value={search}
          onChange={setSearch}
        />
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
          <div className={activeTab === 'images' ? imageTableLayout.minWidth : buildTableLayout.minWidth}>
            {activeTab === 'images' ? (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400", imageTableLayout.grid)}>
                <div className="flex items-center gap-3"><div className="w-10 shrink-0" /><div>{t('images.tag')}</div></div>
                <div>{t('images.size')}</div>
                <div>{t('common.status')}</div>
                <div>{t('images.createdAt')}</div>
                <div className="flex justify-end">{t('common.actions')}</div>
              </div>
            ) : (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400", buildTableLayout.grid)}>
                <div>ID</div>
                <div>{t('common.status')}</div>
                <div>{t('images.duration')}</div>
                <div>{t('images.startedAt')}</div>
                <div className="flex justify-end">{t('common.actions')}</div>
              </div>
            )}

            <div className="flex flex-col">
              {isLoading ? (
                <LoadingRows columns={[1.5, 0.6, 0.6, 0.8, 0.4]} />
              ) : activeTab === 'images' && isImagesError ? (
                <ErrorState title={t('images.loadFailed')} message={failureReason(imagesError, t)} onRetry={() => refetchImages()} isRetrying={isFetchingImages} />
              ) : activeTab === 'builds' && isBuildsError ? (
                <ErrorState title={t('images.loadBuildsFailed')} message={failureReason(buildsError, t)} onRetry={() => refetchBuilds()} isRetrying={isFetchingBuilds} />
              ) : activeTab === 'images' ? (
                filteredImages.length > 0 ? (
                  filteredImages.map((image) => (
                    <ImageRow key={image.id} image={image} />
                  ))
                ) : search && images.length > 0 ? (
                  <NoMatchesState query={search} onClear={() => setSearch('')} />
                ) : (
                  <EmptyState
                    title={t('images.noImages')}
                    description={t('images.noImagesDescription')}
                    action={isBuildDisabled ? undefined : { label: t('images.buildFirst'), icon: Plus, onClick: () => setIsCreateModalOpen(true) }}
                  />
                )
              ) : (
                filteredBuilds.length > 0 ? (
                  filteredBuilds.map((build) => (
                    <BuildRow key={build.id} build={build} onViewLogs={setViewLogsBuild} />
                  ))
                ) : search && builds.length > 0 ? (
                  <NoMatchesState query={search} onClear={() => setSearch('')} />
                ) : (
                  <EmptyState
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
        isOpen={isCreateModalOpen && !isBuildDisabled}
        onClose={() => setIsCreateModalOpen(false)}
        onSuccessSwitchTab={() => setActiveTab('builds')}
      />

      <BuildLogsModal 
        build={viewLogsBuild}
        scope="own"
        onClose={() => setViewLogsBuild(null)}
      />
    </Page>
  );
}
