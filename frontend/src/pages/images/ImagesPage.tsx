import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus, RefreshCcw, Search, Layers, Hammer } from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Pagination } from '@/components/ui/Pagination';
import { ImageRow } from '@/features/images/components/ImageRow';
import { BuildRow } from '@/features/images/components/BuildRow';
import { CreateBuildModal } from '@/features/images/components/CreateBuildModal';
import { BuildLogsModal } from '@/features/images/components/BuildLogsModal';
import { getImagesFn, getBuildsFn } from '@/features/images/api';
import { type BuildData } from '@/features/images/types';
import { cn } from '@/lib/utils';

type Tab = 'images' | 'builds';

export function ImagesPage() {
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

  const filteredImages = images.filter(i => i.tag.toLowerCase().includes(search.toLowerCase()));
  const filteredBuilds = builds.filter(b => b.id.toLowerCase().includes(search.toLowerCase()));

  const totalSizeMB = images.reduce((sum, img) => sum + img.size_mb, 0);

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
          <h1 className="text-3xl font-bold tracking-tight">Образы и Сборки</h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">
            На странице: <span className="font-semibold text-slate-900 dark:text-slate-100">{totalSizeMB} MB</span>
          </p>
        </div>
        
        <div className="flex items-center gap-3 w-full sm:w-auto">
          <div className="relative w-full sm:w-64">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400" />
            <Input 
              placeholder="Поиск..." 
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-9"
            />
          </div>
          <Button variant="secondary" onClick={handleRefresh} isLoading={isFetching} className="px-3">
            <RefreshCcw className="w-4 h-4" />
          </Button>
          <Button onClick={() => setIsCreateModalOpen(true)}>
            <Plus className="w-4 h-4 mr-2" />
            Собрать
          </Button>
        </div>
      </div>

      <div className="flex bg-white/40 dark:bg-slate-900/40 backdrop-blur-md p-1 rounded-xl w-fit border border-white/50 dark:border-slate-700/50 shrink-0">
        <button
          onClick={() => setActiveTab('images')}
          className={cn(
            "flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all",
            activeTab === 'images' 
              ? "bg-white dark:bg-slate-800 shadow-sm text-indigo-600 dark:text-indigo-400" 
              : "text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-100"
          )}
        >
          <Layers className="w-4 h-4" />
          Мои образы
        </button>
        <button
          onClick={() => setActiveTab('builds')}
          className={cn(
            "flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all",
            activeTab === 'builds' 
              ? "bg-white dark:bg-slate-800 shadow-sm text-indigo-600 dark:text-indigo-400" 
              : "text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-100"
          )}
        >
          <Hammer className="w-4 h-4" />
          История сборок
        </button>
      </div>

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col">
        <div className="overflow-x-auto flex-1">
          <div className={activeTab === 'images' ? 'min-w-[800px]' : 'min-w-[800px]'}>
            {activeTab === 'images' ? (
              <div className="grid grid-cols-[3fr_1fr_1fr_1.5fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400">
                <div className="flex items-center gap-3"><div className="w-10 shrink-0" /><div>Тег</div></div>
                <div>Размер</div>
                <div>Статус</div>
                <div>Дата создания</div>
                <div className="text-right pr-2">Действия</div>
              </div>
            ) : (
              <div className="grid grid-cols-[1.5fr_1fr_1fr_1.5fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500 dark:text-slate-400">
                <div>ID</div>
                <div>Статус</div>
                <div>Длительность</div>
                <div>Дата запуска</div>
                <div className="text-right pr-2">Действия</div>
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
                    <h3 className="text-xl font-semibold mb-2">Нет образов</h3>
                    <p className="text-slate-500 dark:text-slate-400 max-w-sm mb-6">
                      У вас пока нет собранных образов. Инициируйте новую сборку, загрузив архив с кодом.
                    </p>
                    <Button onClick={() => setIsCreateModalOpen(true)}>Собрать первый образ</Button>
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
                    <h3 className="text-xl font-semibold mb-2">История пуста</h3>
                    <p className="text-slate-500 dark:text-slate-400 max-w-sm">
                      Вы еще не запускали сборки образов.
                    </p>
                  </div>
                )
              )}
            </div>
          </div>
        </div>
      </div>
      {activeTab === 'images' && imagesData && (
        <Pagination currentPage={imagesPage} pageSize={limit} totalItems={imagesData.total_count} onPageChange={setImagesPage} />
      )}
      {activeTab === 'builds' && buildsData && (
        <Pagination currentPage={buildsPage} pageSize={limit} totalItems={buildsData.total_count} onPageChange={setBuildsPage} />
      )}

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
