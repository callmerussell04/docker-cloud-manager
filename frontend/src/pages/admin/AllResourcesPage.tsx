import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, RefreshCcw, ShieldAlert, Box, HardDrive, Layers, Disc, Trash2, Square, Play, Activity, Terminal, ScrollText, Hammer, XCircle } from 'lucide-react';
import { format } from 'date-fns';
import { Link } from 'react-router-dom';

import { Button } from '@/components/ui/Button';
import { Pagination } from '@/components/ui/Pagination';
import { Badge } from '@/components/ui/Badge';
import { useToastStore } from '@/store/toastStore';
import { cn } from '@/lib/utils';
import { 
  getAllContainersFn, getAllVolumesFn, getAllImagesFn, getAllProjectsFn, getAllBuildsFn,
  adminActionContainerFn, adminDeleteVolumeFn, adminDeleteImageFn, adminActionProjectFn, adminDeleteBuildFn, adminCancelBuildFn
} from '@/features/admin/api';
import { ContainerLogsModal } from '@/features/containers/components/ContainerLogsModal';
import { ContainerTerminalModal } from '@/features/containers/components/ContainerTerminalModal';
import { type AdminContainerData } from '@/features/containers/types';
import { BuildLogsModal } from '@/features/images/components/BuildLogsModal';
import type { AdminBuildData } from '@/features/images/types';
import { getApiErrorMessage } from '@/lib/apiError';
import { tableLayouts } from '@/components/ui/tableLayouts';

type Tab = 'containers' | 'volumes' | 'images' | 'builds' | 'projects';
type BadgeVariant = 'default' | 'success' | 'warning' | 'error' | 'info';
const terminalBuildStatuses = new Set(['success', 'failed', 'failed_timeout', 'failed_quota_exceeded', 'failed_internal', 'canceled']);

const adminTabMinWidths: Record<Tab, string> = {
  containers: tableLayouts.adminContainers.minWidth,
  volumes: tableLayouts.adminVolumes.minWidth,
  images: tableLayouts.adminImages.minWidth,
  builds: tableLayouts.adminBuilds.minWidth,
  projects: tableLayouts.adminProjects.minWidth,
};

function statusVariant(status: string): BadgeVariant {
  if (['running', 'success', 'available', 'active'].includes(status)) return 'success';
  if (['pending', 'creating', 'starting', 'reconciling', 'deploying', 'building'].includes(status)) return 'info';
  if (['stopping', 'deleting', 'canceled'].includes(status)) return 'warning';
  if (status === 'missing' || status === 'failed' || status.startsWith('failed') || status === 'error') return 'error';
  return 'default';
}

function isContainerActionBlocked(status: string) {
  return ['creating', 'starting', 'stopping', 'deleting', 'missing', 'reconciling'].includes(status);
}

export function AllResourcesPage() {
  const [activeTab, setActiveTab] = useState<Tab>('containers');
  const[page, setPage] = useState(1);
  const limit = 20;

  const[logsContainer, setLogsContainer] = useState<AdminContainerData | null>(null);
  const [terminalContainer, setTerminalContainer] = useState<AdminContainerData | null>(null);
  const [viewLogsBuild, setViewLogsBuild] = useState<AdminBuildData | null>(null);

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

  const { data: buildsData, isFetching: isFetchingBuilds } = useQuery({
    queryKey: ['admin_builds', page],
    queryFn: () => getAllBuildsFn(page, limit),
    enabled: activeTab === 'builds',
    refetchInterval: activeTab === 'builds' ? 5000 : false,
  });

  const actionContainerMut = useMutation({
    mutationFn: adminActionContainerFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_containers'] });
      addToast('Действие выполнено', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось выполнить действие');
      addToast(message, 'error', { requestId });
    }
  });

  const delVolMut = useMutation({
    mutationFn: adminDeleteVolumeFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_volumes'] });
      addToast('Том удален', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось удалить том');
      addToast(message, 'error', { requestId });
    }
  });

  const delImgMut = useMutation({
    mutationFn: adminDeleteImageFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_images'] });
      addToast('Образ удален', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось удалить образ');
      addToast(message, 'error', { requestId });
    }
  });

  const actionProjMut = useMutation({
    mutationFn: adminActionProjectFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey:['admin_projects'] });
      addToast('Действие выполнено', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось выполнить действие');
      addToast(message, 'error', { requestId });
    }
  });

  const delBuildMut = useMutation({
    mutationFn: adminDeleteBuildFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_builds'] });
      addToast('Сборка удалена', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось удалить сборку');
      addToast(message, 'error', { requestId });
    }
  });

  const cancelBuildMut = useMutation({
    mutationFn: adminCancelBuildFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_builds'] });
      addToast('Отмена сборки запрошена', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось отменить сборку');
      addToast(message, 'error', { requestId });
    }
  });

  const isFetching = isFetchingCont || isFetchingVol || isFetchingImg || isFetchingProj || isFetchingBuilds;

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

      <div className="max-w-full overflow-x-auto pb-1 shrink-0">
        <div className="flex w-max bg-white/40 dark:bg-slate-900/40 backdrop-blur-md p-1 rounded-xl border border-white/50 dark:border-slate-700/50">
        {[
          { id: 'containers', label: 'Контейнеры', icon: Box },
          { id: 'volumes', label: 'Тома', icon: HardDrive },
          { id: 'images', label: 'Образы', icon: Disc },
          { id: 'builds', label: 'Сборки', icon: Hammer },
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
      </div>

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col relative">
        <div className="overflow-x-auto flex-1">
          <div className={cn("flex flex-col h-full", adminTabMinWidths[activeTab])}>
            
            {activeTab === 'containers' && (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminContainers.grid)}>
                <div className="pl-2">Контейнер / ID</div>
                <div>Статус</div>
                <div>User ID / Docker ID</div>
                <div className="flex justify-end">Управление</div>
              </div>
            )}
            {activeTab === 'volumes' && (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminVolumes.grid)}>
                <div className="pl-2">Имя тома</div>
                <div>Статус / Дата / User</div>
                <div className="flex justify-end">Управление</div>
              </div>
            )}
            {activeTab === 'images' && (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminImages.grid)}>
                <div className="pl-2">Тег</div>
                <div>Размер / Статус</div>
                <div>User ID</div>
                <div className="flex justify-end">Управление</div>
              </div>
            )}
            {activeTab === 'builds' && (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminBuilds.grid)}>
                <div className="pl-2">Build ID</div>
                <div>Статус</div>
                <div>Image ID</div>
                <div>User</div>
                <div>Дата запуска</div>
                <div className="flex justify-end">Управление</div>
              </div>
            )}
            {activeTab === 'projects' && (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminProjects.grid)}>
                <div className="pl-2">Проект</div>
                <div>Статус</div>
                <div>User ID</div>
                <div className="flex justify-end">Управление</div>
              </div>
            )}

            <div className="flex flex-col flex-1">
              {isFetching ? (
                <div className="p-12 flex justify-center opacity-50"><RefreshCcw className="w-8 h-8 animate-spin" /></div>
              ) : (
                <>
                  {activeTab === 'containers' && containersData?.items.map((c) => (
                    <div key={c.id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", tableLayouts.adminContainers.grid)}>
                      <div className="min-w-0 pr-4 pl-2">
                        <Link to={`/admin/containers/${c.id}`} state={{ container: c }} className="font-medium block truncate hover:underline text-slate-900 dark:text-slate-100">{c.name}</Link>
                        <div className="text-xs text-slate-500 block truncate font-mono">{c.id}</div>
                      </div>
                      <div className="min-w-0">
                        <Badge variant={statusVariant(c.status)}>{c.status}</Badge>
                        {c.last_error && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-red-600 dark:text-red-400" title={c.last_error}>
                            <AlertTriangle className="h-3 w-3 shrink-0" />
                            <span className="truncate">{c.last_error}</span>
                          </div>
                        )}
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 space-y-1">
                        <div className="block truncate" title={c.owner_username}>User: {c.owner_username || 'unknown'}</div>
                        <div className="block truncate text-slate-400">Doc: {c.docker_id?.slice(0, 12) || 'N/A'}</div>
                      </div>
                      <div className="flex gap-2 justify-end shrink-0">
                        <Link to={`/admin/containers/${c.id}`} state={{ container: c }} className="p-2 rounded-lg bg-indigo-100 text-indigo-700 hover:bg-indigo-200 dark:bg-indigo-900/30 dark:text-indigo-400 dark:hover:bg-indigo-900/50 transition-colors">
                          <Activity className="w-4 h-4" />
                        </Link>

                        <button
                          onClick={() => setTerminalContainer(c)}
                          disabled={c.status !== 'running'}
                          className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-400 dark:hover:bg-slate-700 transition-colors"
                        >
                          <Terminal className="w-4 h-4" />
                        </button>

                        <button
                          onClick={() => setLogsContainer(c)}
                          className="p-2 rounded-lg bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-400 dark:hover:bg-slate-700 transition-colors"
                        >
                          <ScrollText className="w-4 h-4" />
                        </button>

                        <div className="w-px h-6 bg-slate-200 dark:bg-slate-700 mx-1" />

                        <Button variant="secondary" className="h-8 px-2" disabled={actionContainerMut.isPending || c.status === 'running' || isContainerActionBlocked(c.status)} onClick={() => actionContainerMut.mutate({id: c.id, action: 'start'})}><Play className="w-4 h-4 text-green-500"/></Button>
                        <Button variant="secondary" className="h-8 px-2" disabled={actionContainerMut.isPending || c.status !== 'running'} onClick={() => actionContainerMut.mutate({id: c.id, action: 'stop'})}><Square className="w-4 h-4 text-yellow-500"/></Button>
                        <Button
                          variant="danger"
                          className="h-8 px-2"
                          disabled={actionContainerMut.isPending || c.status === 'deleting'}
                          onClick={() => {
                            if (window.confirm(`Удалить контейнер "${c.name}"?`)) {
                              actionContainerMut.mutate({ id: c.id, action: 'delete' });
                            }
                          }}
                        >
                          <Trash2 className="w-4 h-4"/>
                        </Button>
                      </div>
                    </div>
                  ))}

                  {activeTab === 'volumes' && volumesData?.items.map((v) => (
                    <div key={v.id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", tableLayouts.adminVolumes.grid)}>
                      <div className="min-w-0 pr-4 pl-2">
                        <div className="font-medium block truncate font-mono">{v.docker_name}</div>
                        <div className="text-xs text-slate-500 block truncate font-mono">{v.id}</div>
                        {v.last_error && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-red-600 dark:text-red-400" title={v.last_error}>
                            <AlertTriangle className="h-3 w-3 shrink-0" />
                            <span className="truncate">{v.last_error}</span>
                          </div>
                        )}
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 space-y-1 pr-4">
                        <Badge variant={statusVariant(v.status)}>{v.status}</Badge>
                        <div className="block truncate">{format(v.created_at * 1000, 'dd.MM.yyyy HH:mm')}</div>
                        <div className="block truncate" title={v.owner_username}>User: {v.owner_username || 'unknown'}</div>
                      </div>
                      <div className="flex gap-2 justify-end shrink-0">
                        <Button
                          variant="danger"
                          className="h-8 px-2"
                          disabled={delVolMut.isPending}
                          onClick={() => {
                            if (window.confirm(`Удалить том "${v.docker_name || v.id}"?`)) {
                              delVolMut.mutate(v.id);
                            }
                          }}
                        >
                          <Trash2 className="w-4 h-4"/>
                        </Button>
                      </div>
                    </div>
                  ))}

                  {activeTab === 'images' && imagesData?.items.map((img) => (
                    <div key={img.id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", tableLayouts.adminImages.grid)}>
                      <div className="min-w-0 pr-4 pl-2">
                        <div className="font-medium block truncate" title={img.tag}>{img.tag}</div>
                        {img.last_error && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-red-600 dark:text-red-400" title={img.last_error}>
                            <AlertTriangle className="h-3 w-3 shrink-0" />
                            <span className="truncate">{img.last_error}</span>
                          </div>
                        )}
                      </div>
                      <div className="min-w-0">
                        <Badge variant="info">{img.size_mb} MB</Badge>
                        <div className="mt-1">
                          <Badge variant={statusVariant(img.status)}>{img.status}</Badge>
                        </div>
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 pr-4">
                        <div className="block truncate" title={img.owner_username}>User: {img.owner_username || 'unknown'}</div>
                      </div>
                      <div className="flex gap-2 justify-end shrink-0">
                        <Button
                          variant="danger"
                          className="h-8 px-2"
                          disabled={delImgMut.isPending}
                          onClick={() => {
                            if (window.confirm(`Удалить образ "${img.tag}"?`)) {
                              delImgMut.mutate(img.id);
                            }
                          }}
                        >
                          <Trash2 className="w-4 h-4"/>
                        </Button>
                      </div>
                    </div>
                  ))}

                  {activeTab === 'builds' && buildsData?.items.map((build) => {
                    const canCancel = build.status === 'pending' || build.status === 'running';
                    const canViewLogs = terminalBuildStatuses.has(build.status);
                    return (
                      <div key={build.id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", tableLayouts.adminBuilds.grid)}>
                        <div className="min-w-0 pr-4 pl-2">
                          <div className="font-medium block truncate font-mono" title={build.id}>{build.id}</div>
                        </div>
                        <div className="min-w-0">
                        <Badge variant={statusVariant(build.status)}>{build.status}</Badge>
                        </div>
                        <div className="min-w-0 text-xs font-mono text-slate-500 truncate" title={build.image_id}>{build.image_id}</div>
                        <div className="min-w-0 text-xs font-mono text-slate-500 truncate" title={build.owner_username}>User: {build.owner_username || build.owner_id || 'unknown'}</div>
                        <div className="min-w-0 text-xs text-slate-500 truncate">{format(build.started_at * 1000, 'dd.MM.yyyy HH:mm')}</div>
                        <div className="flex gap-2 justify-end shrink-0">
                          <Button
                            variant="secondary"
                            className="h-8 px-2"
                            disabled={!canViewLogs}
                            title={canViewLogs ? 'Просмотр логов' : 'Логи доступны после завершения сборки'}
                            onClick={() => setViewLogsBuild(build)}
                          >
                            <ScrollText className="w-4 h-4" />
                          </Button>
                          <Button
                            variant="secondary"
                            className="h-8 px-2"
                            disabled={!canCancel || cancelBuildMut.isPending}
                            onClick={() => {
                              if (window.confirm(`Отменить сборку "${build.id}"?`)) {
                                cancelBuildMut.mutate(build.id);
                              }
                            }}
                          >
                            <XCircle className="w-4 h-4 text-yellow-500" />
                          </Button>
                          <Button
                            variant="danger"
                            className="h-8 px-2"
                            disabled={canCancel || delBuildMut.isPending}
                            onClick={() => {
                              if (window.confirm(`Удалить запись сборки "${build.id}"?`)) {
                                delBuildMut.mutate(build.id);
                              }
                            }}
                          >
                            <Trash2 className="w-4 h-4"/>
                          </Button>
                        </div>
                      </div>
                    );
                  })}

                  {activeTab === 'projects' && projectsData?.items.map((p) => {
                    const isProjectBusy = ['building', 'deploying', 'pending', 'starting', 'stopping', 'deleting'].includes(p.status);
                    return (
                    <div key={p.id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", tableLayouts.adminProjects.grid)}>
                      <div className="min-w-0 pr-4 pl-2">
                        <div className="font-medium block truncate" title={p.name}>{p.name}</div>
                        <div className="text-xs text-slate-500 block truncate font-mono">{p.id}</div>
                      </div>
                      <div className="min-w-0">
                        <Badge variant={statusVariant(p.status)}>
                          {p.status === 'stopped' ? 'Остановлен' : p.status}
                        </Badge>
                        {p.last_error && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-red-600 dark:text-red-400" title={p.last_error}>
                            <AlertTriangle className="h-3 w-3 shrink-0" />
                            <span className="truncate">{p.last_error}</span>
                          </div>
                        )}
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 space-y-1 pr-4">
                        <div className="block truncate">{format(p.created_at * 1000, 'dd.MM.yyyy HH:mm')}</div>
                        <div className="block truncate" title={p.owner_username}>User: {p.owner_username || 'unknown'}</div>
                      </div>
                      <div className="flex gap-2 justify-end shrink-0">
                        <Button 
                          variant="secondary" 
                          className="h-8 px-2" 
                          disabled={actionProjMut.isPending || isProjectBusy || p.status === 'running'}
                          onClick={() => actionProjMut.mutate({id: p.id, action: 'start'})}
                        >
                          <Play className="w-4 h-4 text-green-500"/>
                        </Button>
                        <Button 
                          variant="secondary" 
                          className="h-8 px-2" 
                          disabled={actionProjMut.isPending || isProjectBusy || p.status === 'failed' || p.status === 'stopped'}
                          onClick={() => actionProjMut.mutate({id: p.id, action: 'stop'})}
                        >
                          <Square className="w-4 h-4 text-yellow-500"/>
                        </Button>
                        <Button
                          variant="danger"
                          className="h-8 px-2"
                          disabled={actionProjMut.isPending || isProjectBusy}
                          onClick={() => {
                            if (window.confirm(`Удалить проект "${p.name}"?`)) {
                              actionProjMut.mutate({ id: p.id, action: 'delete' });
                            }
                          }}
                        >
                          <Trash2 className="w-4 h-4"/>
                        </Button>
                      </div>
                    </div>
                  );
                  })}
                </>
              )}
            </div>

            <div className="mt-auto shrink-0 bg-slate-50/50 dark:bg-slate-800/50 rounded-b-2xl">
              {activeTab === 'containers' && containersData && <Pagination currentPage={page} pageSize={limit} totalItems={containersData.total_count} onPageChange={setPage} />}
              {activeTab === 'volumes' && volumesData && <Pagination currentPage={page} pageSize={limit} totalItems={volumesData.total_count} onPageChange={setPage} />}
              {activeTab === 'images' && imagesData && <Pagination currentPage={page} pageSize={limit} totalItems={imagesData.total_count} onPageChange={setPage} />}
              {activeTab === 'builds' && buildsData && <Pagination currentPage={page} pageSize={limit} totalItems={buildsData.total_count} onPageChange={setPage} />}
              {activeTab === 'projects' && projectsData && <Pagination currentPage={page} pageSize={limit} totalItems={projectsData.total_count} onPageChange={setPage} />}
            </div>
          </div>
        </div>
      </div>

      <ContainerLogsModal 
        container={logsContainer}
        isAdmin={true}
        onClose={() => setLogsContainer(null)}
      />

      <ContainerTerminalModal
        container={terminalContainer}
        isAdmin={true}
        onClose={() => setTerminalContainer(null)}
      />

      <BuildLogsModal
        build={viewLogsBuild}
        isAdmin={true}
        onClose={() => setViewLogsBuild(null)}
      />

    </div>
  );
}
