import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus, RefreshCcw, Search, Layers, Hammer } from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { ImageCard } from '@/features/images/components/ImageCard';
import { BuildCard } from '@/features/images/components/BuildCard';
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

  const { data: images = [], isLoading: isLoadingImages, refetch: refetchImages, isFetching: isFetchingImages } = useQuery({
    queryKey: ['images'],
    queryFn: getImagesFn,
  });

  const { data: builds = [], isLoading: isLoadingBuilds, refetch: refetchBuilds, isFetching: isFetchingBuilds } = useQuery({
    queryKey: ['builds'],
    queryFn: getBuildsFn,
    refetchInterval: activeTab === 'builds' ? 5000 : false, // Автообновление для сборок
  });

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
    <div className="space-y-6">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Образы и Сборки</h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">
            Занято места: <span className="font-semibold text-slate-900 dark:text-slate-100">{totalSizeMB} MB</span>
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

      <div className="flex bg-white/40 dark:bg-slate-900/40 backdrop-blur-md p-1 rounded-xl w-fit border border-white/50 dark:border-slate-700/50">
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

      {isLoading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6">
          {[...Array(8)].map((_, i) => (
            <div key={i} className="h-40 bg-white/40 dark:bg-slate-900/40 rounded-2xl animate-pulse" />
          ))}
        </div>
      ) : activeTab === 'images' ? (
        filteredImages.length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6">
            {filteredImages.map((image) => (
              <ImageCard key={image.id} image={image} />
            ))}
          </div>
        ) : (
          <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl p-12 flex flex-col items-center justify-center text-center">
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
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6">
            {filteredBuilds.map((build) => (
              <BuildCard key={build.id} build={build} onViewLogs={setViewLogsBuild} />
            ))}
          </div>
        ) : (
          <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl p-12 flex flex-col items-center justify-center text-center">
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