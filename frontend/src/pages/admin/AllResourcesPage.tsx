/* eslint-disable @typescript-eslint/no-explicit-any */
import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { RefreshCcw, ShieldAlert, Box, HardDrive, Layers, Disc, Trash2, Square, Play, Activity } from 'lucide-react';
import { format } from 'date-fns';
import { Link } from 'react-router-dom';

import { Button } from '@/components/ui/Button';
import { Pagination } from '@/components/ui/Pagination';
import { Badge } from '@/components/ui/Badge';
import { useToastStore } from '@/store/toastStore';
import { cn } from '@/lib/utils';
import { 
  getAllContainersFn, getAllVolumesFn, getAllImagesFn, getAllProjectsFn,
  adminActionContainerFn, adminDeleteVolumeFn, adminDeleteImageFn, adminActionProjectFn
} from '@/features/admin/api';

type Tab = 'containers' | 'volumes' | 'images' | 'projects';

export function AllResourcesPage() {
  const [activeTab, setActiveTab] = useState<Tab>('containers');
  const [page, setPage] = useState(1);
  const limit = 20;

  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const handleTabChange = (tab: Tab) => {
    setActiveTab(tab);
    setPage(1);
  };

  const { data: containersData, isFetching: isFetchingCont } = useQuery({
    queryKey: ['admin_containers', page],
    queryFn: () => getAllContainersFn(page, limit),
    enabled: activeTab === 'containers',
  });

  const { data: volumesData, isFetching: isFetchingVol } = useQuery({
    queryKey: ['admin_volumes', page],
    queryFn: () => getAllVolumesFn(page, limit),
    enabled: activeTab === 'volumes',
  });

  const { data: imagesData, isFetching: isFetchingImg } = useQuery({
    queryKey: ['admin_images', page],
    queryFn: () => getAllImagesFn(page, limit),
    enabled: activeTab === 'images',
  });

  const { data: projectsData, isFetching: isFetchingProj } = useQuery({
    queryKey: ['admin_projects', page],
    queryFn: () => getAllProjectsFn(page, limit),
    enabled: activeTab === 'projects',
  });

  const actionContainerMut = useMutation({
    mutationFn: adminActionContainerFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_containers'] });
      addToast('Действие выполнено', 'success');
    }
  });

  const delVolMut = useMutation({
    mutationFn: adminDeleteVolumeFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_volumes'] });
      addToast('Том удален', 'success');
    }
  });

  const delImgMut = useMutation({
    mutationFn: adminDeleteImageFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_images'] });
      addToast('Образ удален', 'success');
    }
  });

  const actionProjMut = useMutation({
    mutationFn: adminActionProjectFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_projects'] });
      addToast('Действие выполнено', 'success');
    }
  });

  const isFetching = isFetchingCont || isFetchingVol || isFetchingImg || isFetchingProj;

  return (
    <div className="space-y-6 flex flex-col h-full">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 shrink-0">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-red-600 dark:text-red-400 flex items-center gap-3">
            <ShieldAlert className="w-8 h-8" />
            Все ресурсы системы
          </h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">Управление ресурсами всех пользователей платформы</p>
        </div>
        
        <Button variant="secondary" onClick={() => queryClient.invalidateQueries()} isLoading={isFetching} className="px-3">
          <RefreshCcw className="w-4 h-4 mr-2" /> Обновить
        </Button>
      </div>

      <div className="flex bg-white/40 dark:bg-slate-900/40 backdrop-blur-md p-1 rounded-xl w-fit border border-white/50 dark:border-slate-700/50 shrink-0">
        {[
          { id: 'containers', label: 'Контейнеры', icon: Box },
          { id: 'volumes', label: 'Тома', icon: HardDrive },
          { id: 'images', label: 'Образы', icon: Disc },
          { id: 'projects', label: 'Проекты', icon: Layers },
        ].map((tab) => (
          <button
            key={tab.id}
            onClick={() => handleTabChange(tab.id as Tab)}
            className={cn(
              "flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-all",
              activeTab === tab.id 
                ? "bg-white dark:bg-slate-800 shadow-sm text-red-600 dark:text-red-400" 
                : "text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-100"
            )}
          >
            <tab.icon className="w-4 h-4" />
            {tab.label}
          </button>
        ))}
      </div>

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col relative">
        <div className="overflow-x-auto flex-1">
          <div className="min-w-[900px] flex flex-col h-full">
            
            {activeTab === 'containers' && (
              <div className="grid grid-cols-[2fr_1fr_2fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500">
                <div className="pl-1">Контейнер / ID</div>
                <div>Статус</div>
                <div>User ID / Docker ID</div>
                <div className="text-right pr-2">Управление</div>
              </div>
            )}
            {activeTab === 'volumes' && (
              <div className="grid grid-cols-[2fr_1fr_1.5fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500">
                <div className="pl-1">Имя тома</div>
                <div>Драйвер</div>
                <div>Дата / User ID</div>
                <div className="text-right pr-2">Управление</div>
              </div>
            )}
            {activeTab === 'images' && (
              <div className="grid grid-cols-[2fr_1fr_1fr_1.5fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500">
                <div className="pl-1">Тег</div>
                <div>Размер</div>
                <div>Тип</div>
                <div>User ID</div>
                <div className="text-right pr-2">Управление</div>
              </div>
            )}
            {activeTab === 'projects' && (
              <div className="grid grid-cols-[2fr_1fr_2fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500">
                <div className="pl-1">Проект</div>
                <div>Статус</div>
                <div>User ID</div>
                <div className="text-right pr-2">Управление</div>
              </div>
            )}

            <div className="flex flex-col flex-1">
              {isFetching ? (
                <div className="p-12 flex justify-center opacity-50"><RefreshCcw className="w-8 h-8 animate-spin" /></div>
              ) : (
                <>
                  {activeTab === 'containers' && containersData?.items.map((c) => (
                    <div key={c.id} className="grid grid-cols-[2fr_1fr_2fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center">
                      <div className="min-w-0">
                        <Link to={`/admin/containers/${c.id}`} state={{ container: c }} className="font-medium truncate hover:underline text-slate-900 dark:text-slate-100">{c.name}</Link>
                        <div className="text-xs text-slate-500 truncate font-mono">{c.id}</div>
                      </div>
                      <div>
                        <Badge variant={c.status === 'running' ? 'success' : 'default'}>{c.status}</Badge>
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 space-y-1">
                        <div className="truncate">User: {(c as any).owner_id || 'unknown'}</div>
                        <div className="truncate text-slate-400">Doc: {c.docker_id?.slice(0, 12) || 'N/A'}</div>
                      </div>
                      <div className="flex gap-2 justify-end">
                        <Link to={`/admin/containers/${c.id}`} state={{ container: c }} className="p-2 rounded-lg bg-indigo-100 text-indigo-700 hover:bg-indigo-200 dark:bg-indigo-900/30 dark:text-indigo-400 dark:hover:bg-indigo-900/50 transition-colors">
                          <Activity className="w-4 h-4" />
                        </Link>
                        <Button variant="secondary" className="h-8 px-2" disabled={actionContainerMut.isPending} onClick={() => actionContainerMut.mutate({id: c.id, action: 'start'})}><Play className="w-4 h-4 text-green-500"/></Button>
                        <Button variant="secondary" className="h-8 px-2" disabled={actionContainerMut.isPending} onClick={() => actionContainerMut.mutate({id: c.id, action: 'stop'})}><Square className="w-4 h-4 text-yellow-500"/></Button>
                        <Button variant="danger" className="h-8 px-2" disabled={actionContainerMut.isPending} onClick={() => actionContainerMut.mutate({id: c.id, action: 'delete'})}><Trash2 className="w-4 h-4"/></Button>
                      </div>
                    </div>
                  ))}

                  {activeTab === 'volumes' && volumesData?.items.map((v) => (
                    <div key={v.id} className="grid grid-cols-[2fr_1fr_1.5fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center">
                      <div className="min-w-0">
                        <div className="font-medium truncate font-mono">{v.docker_name}</div>
                        <div className="text-xs text-slate-500 truncate font-mono">{v.id}</div>
                      </div>
                      <div>
                        <Badge variant="info">{v.driver}</Badge>
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 space-y-1">
                        <div>{format(v.created_at * 1000, 'dd.MM.yyyy HH:mm')}</div>
                        <div className="truncate">User: {(v as any).owner_id || 'unknown'}</div>
                      </div>
                      <div className="flex gap-2 justify-end">
                        <Button variant="danger" className="h-8 px-2" disabled={delVolMut.isPending} onClick={() => delVolMut.mutate(v.id)}><Trash2 className="w-4 h-4"/></Button>
                      </div>
                    </div>
                  ))}

                  {activeTab === 'images' && imagesData?.items.map((img) => (
                    <div key={img.id} className="grid grid-cols-[2fr_1fr_1fr_1.5fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center">
                      <div className="min-w-0">
                        <div className="font-medium truncate">{img.tag}</div>
                      </div>
                      <div>
                        <Badge variant="info">{img.size_mb} MB</Badge>
                      </div>
                      <div>
                        <Badge variant={img.is_custom ? 'warning' : 'default'}>{img.is_custom ? 'Custom' : 'System'}</Badge>
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500">
                        <div className="truncate">User: {(img as any).owner_id || 'unknown'}</div>
                      </div>
                      <div className="flex gap-2 justify-end">
                        {img.is_custom && (
                          <Button variant="danger" className="h-8 px-2" disabled={delImgMut.isPending} onClick={() => delImgMut.mutate(img.id)}><Trash2 className="w-4 h-4"/></Button>
                        )}
                      </div>
                    </div>
                  ))}

                  {activeTab === 'projects' && projectsData?.items.map((p) => (
                    <div key={p.id} className="grid grid-cols-[2fr_1fr_2fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center">
                      <div className="min-w-0">
                        <div className="font-medium truncate">{p.name}</div>
                        <div className="text-xs text-slate-500 truncate font-mono">{p.id}</div>
                      </div>
                      <div>
                        <Badge variant={p.status === 'running' ? 'success' : p.status === 'failed' ? 'error' : 'default'}>
                          {p.status === 'stopped' ? 'Остановлен' : p.status}
                        </Badge>
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 space-y-1">
                        <div>{format(p.created_at * 1000, 'dd.MM.yyyy HH:mm')}</div>
                        <div className="truncate">User: {(p as any).owner_id || 'unknown'}</div>
                      </div>
                      <div className="flex gap-2 justify-end">
                        <Button 
                          variant="secondary" 
                          className="h-8 px-2" 
                          disabled={actionProjMut.isPending || p.status === 'stopped'} 
                          onClick={() => actionProjMut.mutate({id: p.id, action: 'stop'})}
                        >
                          <Square className="w-4 h-4 text-yellow-500"/>
                        </Button>
                        <Button 
                          variant="danger" 
                          className="h-8 px-2" 
                          disabled={actionProjMut.isPending} 
                          onClick={() => actionProjMut.mutate({id: p.id, action: 'delete'})}
                        >
                          <Trash2 className="w-4 h-4"/>
                        </Button>
                      </div>
                    </div>
                  ))}
                </>
              )}
            </div>

            <div className="mt-auto shrink-0 bg-slate-50/50 dark:bg-slate-800/50 rounded-b-2xl">
              {activeTab === 'containers' && containersData && <Pagination currentPage={page} pageSize={limit} totalItems={containersData.total_count} onPageChange={setPage} />}
              {activeTab === 'volumes' && volumesData && <Pagination currentPage={page} pageSize={limit} totalItems={volumesData.total_count} onPageChange={setPage} />}
              {activeTab === 'images' && imagesData && <Pagination currentPage={page} pageSize={limit} totalItems={imagesData.total_count} onPageChange={setPage} />}
              {activeTab === 'projects' && projectsData && <Pagination currentPage={page} pageSize={limit} totalItems={projectsData.total_count} onPageChange={setPage} />}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}