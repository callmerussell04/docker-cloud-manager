import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, RefreshCcw, ShieldAlert, Box, HardDrive, Layers, Disc, Trash2, Square, Play, Activity, Terminal, ScrollText, Hammer, XCircle, Globe, ExternalLink } from 'lucide-react';
import { Link } from 'react-router-dom';

import { Button } from '@/components/ui/Button';
import { Pagination } from '@/components/ui/Pagination';
import { Badge } from '@/components/ui/Badge';
import { cn } from '@/lib/utils';
import { ContainerLogsModal } from '@/features/containers/components/ContainerLogsModal';
import { ContainerTerminalModal } from '@/features/containers/components/ContainerTerminalModal';
import { ContainerTTLTimer } from '@/features/containers/components/ContainerTTLTimer';
import { type AdminContainerData } from '@/features/containers/types';
import { BuildLogsModal } from '@/features/images/components/BuildLogsModal';
import type { AdminBuildData } from '@/features/images/types';
import { tableLayouts } from '@/components/ui/tableLayouts';
import { dateLocale, statusLabel, useLocale, useT } from '@/lib/i18n';
import { getApiErrorMessage, getServerErrorMessage } from '@/lib/apiError';
import {
  useAdminBuilds,
  useAdminCancelBuild,
  useAdminContainerAction,
  useAdminContainers,
  useAdminDeleteBuild,
  useAdminDeleteImage,
  useAdminDeleteVolume,
  useAdminImages,
  useAdminProjectAction,
  useAdminProjects,
  useAdminVolumes,
} from '@/features/admin/hooks';
import { queryKeys } from '@/shared/api/queryKeys';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { TabSwitcher } from '@/components/common/TabSwitcher';
import { BASE_DOMAIN } from '@/config';
import type { Locale } from '@/store/languageStore';
import { EmptyState } from '@/components/common/EmptyState';
import { ErrorState } from '@/components/common/ErrorState';

type Tab = 'containers' | 'volumes' | 'images' | 'builds' | 'projects';
type BadgeVariant = 'default' | 'success' | 'warning' | 'error' | 'info';
const terminalBuildStatuses = new Set(['success', 'failed', 'failed_timeout', 'failed_quota_exceeded', 'failed_resource_exhausted', 'failed_internal', 'canceled']);

const adminTabMinWidths: Record<Tab, string> = {
  containers: tableLayouts.adminContainers.minWidth,
  volumes: tableLayouts.adminVolumes.minWidth,
  images: tableLayouts.adminImages.minWidth,
  builds: tableLayouts.adminBuilds.minWidth,
  projects: tableLayouts.adminProjects.minWidth,
};

function statusVariant(status: string): BadgeVariant {
  if (['running', 'success', 'available', 'active'].includes(status)) return 'success';
  if (['pending', 'creating', 'starting', 'exposing', 'reconciling', 'deploying', 'building'].includes(status)) return 'info';
  if (['stopping', 'deleting', 'canceling', 'canceled'].includes(status)) return 'warning';
  if (status === 'missing' || status === 'failed' || status.startsWith('failed') || status === 'error') return 'error';
  return 'default';
}

function isContainerActionBlocked(status: string) {
  return ['creating', 'starting', 'stopping', 'exposing', 'deleting', 'missing', 'reconciling'].includes(status);
}

function formatDateTime(value: number, locale: Locale) {
  if (!value) return '-';
  return new Date(value * 1000).toLocaleString(dateLocale(locale));
}

function buildDurationSeconds(startedAt: number, finishedAt: number) {
  if (!finishedAt) return null;
  return Math.max(0, finishedAt - startedAt);
}

export function AllResourcesPage() {
  const [activeTab, setActiveTab] = useState<Tab>('containers');
  const[page, setPage] = useState(1);
  const limit = 20;

  const[logsContainer, setLogsContainer] = useState<AdminContainerData | null>(null);
  const [terminalContainer, setTerminalContainer] = useState<AdminContainerData | null>(null);
  const [viewLogsBuild, setViewLogsBuild] = useState<AdminBuildData | null>(null);
  const [confirmAction, setConfirmAction] = useState<{
    message: string;
    confirmLabel: string;
    variant?: 'danger' | 'secondary';
    run: () => void;
  } | null>(null);

  const queryClient = useQueryClient();
  const t = useT();
  const locale = useLocale();
  const resourceError = (message?: string) => getServerErrorMessage(message, t);

  const handleTabChange = (tab: Tab) => {
    setActiveTab(tab);
    setPage(1);
  };

  const { data: containersData, isFetching: isFetchingCont, isError: isContainersError, error: containersError, refetch: refetchContainers } = useAdminContainers(page, limit, activeTab === 'containers');
  const { data: volumesData, isFetching: isFetchingVol, isError: isVolumesError, error: volumesError, refetch: refetchVolumes } = useAdminVolumes(page, limit, activeTab === 'volumes');
  const { data: imagesData, isFetching: isFetchingImg, isError: isImagesError, error: imagesError, refetch: refetchImages } = useAdminImages(page, limit, activeTab === 'images');
  const { data: projectsData, isFetching: isFetchingProj, isError: isProjectsError, error: projectsError, refetch: refetchProjects } = useAdminProjects(page, limit, activeTab === 'projects');
  const { data: buildsData, isFetching: isFetchingBuilds, isError: isBuildsError, error: buildsError, refetch: refetchBuilds } = useAdminBuilds(page, limit, activeTab === 'builds');

  const actionContainerMut = useAdminContainerAction();
  const delVolMut = useAdminDeleteVolume();
  const delImgMut = useAdminDeleteImage();
  const actionProjMut = useAdminProjectAction();
  const delBuildMut = useAdminDeleteBuild();
  const cancelBuildMut = useAdminCancelBuild();

  const refreshActiveTab = () => {
    const keys = {
      containers: queryKeys.admin.containers.all,
      volumes: queryKeys.admin.volumes.all,
      images: queryKeys.admin.images.all,
      builds: queryKeys.admin.builds.all,
      projects: queryKeys.admin.projects.all,
    } as const;
    queryClient.invalidateQueries({ queryKey: keys[activeTab] });
  };

  const isFetching = isFetchingCont || isFetchingVol || isFetchingImg || isFetchingProj || isFetchingBuilds;
  const isMutating = actionContainerMut.isPending || delVolMut.isPending || delImgMut.isPending || actionProjMut.isPending || delBuildMut.isPending || cancelBuildMut.isPending;
  const activeError = {
    containers: isContainersError,
    volumes: isVolumesError,
    images: isImagesError,
    builds: isBuildsError,
    projects: isProjectsError,
  }[activeTab];
  const activeErrorMessage = {
    containers: getApiErrorMessage(containersError, t('containers.loadFailed'), t).message,
    volumes: getApiErrorMessage(volumesError, t('volumes.loadFailed'), t).message,
    images: getApiErrorMessage(imagesError, t('images.loadFailed'), t).message,
    builds: getApiErrorMessage(buildsError, t('images.loadBuildsFailed'), t).message,
    projects: getApiErrorMessage(projectsError, t('projects.loadFailed'), t).message,
  }[activeTab];
  const activeErrorTitle = {
    containers: t('containers.loadFailed'),
    volumes: t('volumes.loadFailed'),
    images: t('images.loadFailed'),
    builds: t('images.loadBuildsFailed'),
    projects: t('projects.loadFailed'),
  }[activeTab];
  const activeRetry = {
    containers: refetchContainers,
    volumes: refetchVolumes,
    images: refetchImages,
    builds: refetchBuilds,
    projects: refetchProjects,
  }[activeTab];
  const activeEmpty = {
    containers: { icon: <Box className="h-8 w-8" />, title: t('containers.emptyTitle'), description: t('containers.emptyDescription') },
    volumes: { icon: <HardDrive className="h-8 w-8" />, title: t('volumes.emptyTitle'), description: t('volumes.emptyDescription') },
    images: { icon: <Disc className="h-8 w-8" />, title: t('images.noImages'), description: t('images.noImagesDescription') },
    builds: { icon: <Hammer className="h-8 w-8" />, title: t('images.emptyBuilds'), description: t('images.emptyBuildsDescription') },
    projects: { icon: <Layers className="h-8 w-8" />, title: t('projects.emptyTitle'), description: t('projects.emptyDescription') },
  }[activeTab];
  const activeItemsCount = {
    containers: containersData?.items.length ?? 0,
    volumes: volumesData?.items.length ?? 0,
    images: imagesData?.items.length ?? 0,
    builds: buildsData?.items.length ?? 0,
    projects: projectsData?.items.length ?? 0,
  }[activeTab];

  const requestConfirm = (message: string, run: () => void, confirmLabel = t('common.delete'), variant: 'danger' | 'secondary' = 'danger') => {
    setConfirmAction({ message, run, confirmLabel, variant });
  };

  return (
    <div className="space-y-6 flex flex-col h-full">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 shrink-0">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-red-600 dark:text-red-400 flex items-center gap-3">
            <ShieldAlert className="w-8 h-8" />
            {t('admin.resources.title')}
          </h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">{t('admin.resources.subtitle')}</p>
        </div>
        
        <Button variant="secondary" onClick={refreshActiveTab} isLoading={isFetching} className="px-3">
          <RefreshCcw className="w-4 h-4 mr-2" /> {t('common.refresh')}
        </Button>
      </div>

      <TabSwitcher
        tone="danger"
        activeTab={activeTab}
        onChange={handleTabChange}
        items={[
          { id: 'containers', label: t('nav.containers'), icon: Box },
          { id: 'volumes', label: t('nav.volumes'), icon: HardDrive },
          { id: 'images', label: t('nav.images'), icon: Disc },
          { id: 'builds', label: t('images.buildHistory'), icon: Hammer },
          { id: 'projects', label: t('projects.title'), icon: Layers },
        ]}
      />

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col relative">
        <div className="overflow-x-auto flex-1">
          <div className={cn("flex flex-col h-full", adminTabMinWidths[activeTab])}>
            
            {activeTab === 'containers' && (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminContainers.grid)}>
                <div className="pl-2">{t('containers.name')} / ID</div>
                <div>{t('common.status')}</div>
                <div>{t('containers.routing')}</div>
                <div>{t('admin.resources.owner')} / Docker ID</div>
                <div className="flex justify-end">{t('admin.users.management')}</div>
              </div>
            )}
            {activeTab === 'volumes' && (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminVolumes.grid)}>
                <div className="pl-2">{t('volumes.name')}</div>
                <div>{t('common.status')}</div>
                <div>{t('images.createdAt')}</div>
                <div>{t('admin.resources.owner')}</div>
                <div className="flex justify-end">{t('admin.users.management')}</div>
              </div>
            )}
            {activeTab === 'images' && (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminImages.grid)}>
                <div className="pl-2">{t('images.tag')}</div>
                <div>{t('images.size')}</div>
                <div>{t('common.status')}</div>
                <div>{t('images.createdAt')}</div>
                <div>{t('admin.resources.owner')}</div>
                <div className="flex justify-end">{t('admin.users.management')}</div>
              </div>
            )}
            {activeTab === 'builds' && (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminBuilds.grid)}>
                <div className="pl-2">{t('admin.resources.buildId')}</div>
                <div>{t('common.status')}</div>
                <div>{t('admin.resources.imageId')}</div>
                <div>{t('common.user')}</div>
                <div>{t('images.startedAt')}</div>
                <div>{t('images.duration')}</div>
                <div className="flex justify-end">{t('admin.users.management')}</div>
              </div>
            )}
            {activeTab === 'projects' && (
              <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminProjects.grid)}>
                <div className="pl-2">{t('projects.project')}</div>
                <div>{t('common.status')}</div>
                <div>{t('status.error')}</div>
                <div>{t('projects.createdAt')}</div>
                <div>{t('admin.resources.owner')}</div>
                <div className="flex justify-end">{t('admin.users.management')}</div>
              </div>
            )}

            <div className="flex flex-col flex-1">
              {isFetching ? (
                <div className="p-12 flex justify-center opacity-50"><RefreshCcw className="w-8 h-8 animate-spin" /></div>
              ) : activeError ? (
                <div className="p-4">
                  <ErrorState
                    title={activeErrorTitle}
                    message={activeErrorMessage}
                    onRetry={() => activeRetry()}
                    isRetrying={isFetching}
                  />
                </div>
              ) : activeItemsCount === 0 ? (
                <EmptyState icon={activeEmpty.icon} title={activeEmpty.title} description={activeEmpty.description} />
              ) : (
                <>
                  {activeTab === 'containers' && containersData?.items.map((c) => (
                    <div key={c.id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", tableLayouts.adminContainers.grid)}>
                      <div className="min-w-0 pr-4 pl-2">
                        <Link to={`/admin/containers/${c.id}`} state={{ container: c }} className="font-medium block truncate hover:underline text-slate-900 dark:text-slate-100">{c.name}</Link>
                        <div className="text-xs text-slate-500 block truncate font-mono">{c.id}</div>
                      </div>
                      <div className="min-w-0">
                        <Badge variant={statusVariant(c.status)}>{statusLabel(t, c.status)}</Badge>
                        <ContainerTTLTimer status={c.status} ttlDeadline={c.ttl_deadline} />
                        {resourceError(c.last_error) && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-red-600 dark:text-red-400" title={resourceError(c.last_error)}>
                            <AlertTriangle className="h-3 w-3 shrink-0" />
                            <span className="truncate">{resourceError(c.last_error)}</span>
                          </div>
                        )}
                      </div>
                      <div className="min-w-0 flex items-center text-sm text-slate-600 dark:text-slate-300 pr-4">
                        {c.domain_prefix ? (
                          <a
                            href={`http://${c.domain_prefix}.${BASE_DOMAIN}`}
                            target="_blank"
                            rel="noreferrer"
                            className="inline-flex min-w-0 items-center gap-2 text-indigo-600 transition-colors hover:text-indigo-700 dark:text-indigo-400 dark:hover:text-indigo-300"
                            title={`${c.domain_prefix}.${BASE_DOMAIN}:${c.internal_port}`}
                          >
                            <Globe className="h-4 w-4 shrink-0" />
                            <span className="truncate">{c.domain_prefix}.{BASE_DOMAIN}</span>
                            <ExternalLink className="h-3 w-3 shrink-0 opacity-70" />
                          </a>
                        ) : (
                          <div className="flex min-w-0 items-center gap-2 text-slate-400" title={t('common.notRouted')}>
                            <Globe className="h-4 w-4 shrink-0" />
                            <span className="truncate">{t('common.notRouted')}</span>
                          </div>
                        )}
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 space-y-1">
                        <div className="block truncate" title={c.owner_username || c.owner_id}>{t('admin.resources.userLabel', { value: c.owner_username || c.owner_id || 'unknown' })}</div>
                        <div className="block truncate text-slate-400" title={c.docker_id}>{t('admin.resources.dockerIdLabel', { value: c.docker_id?.slice(0, 12) || 'N/A' })}</div>
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
                          disabled={actionContainerMut.isPending || ['exposing', 'deleting'].includes(c.status)}
                          onClick={() => requestConfirm(t('containers.deleteConfirm', { name: c.name }), () => actionContainerMut.mutate({ id: c.id, action: 'delete' }))}
                        >
                          <Trash2 className="w-4 h-4"/>
                        </Button>
                      </div>
                    </div>
                  ))}

                  {activeTab === 'volumes' && volumesData?.items.map((v) => (
                    <div key={v.id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", tableLayouts.adminVolumes.grid)}>
                      <div className="min-w-0 pr-4 pl-2">
                        <div className="font-medium block truncate font-mono">{v.name || v.id}</div>
                        <div className="text-xs text-slate-500 block truncate font-mono" title={v.docker_name}>{v.docker_name}</div>
                        <div className="text-xs text-slate-500 block truncate font-mono">{v.id}</div>
                      </div>
                      <div className="min-w-0 text-sm">
                        <Badge variant={statusVariant(v.status)}>{statusLabel(t, v.status)}</Badge>
                        {v.last_error && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-red-600 dark:text-red-400" title={v.last_error}>
                            <AlertTriangle className="h-3 w-3 shrink-0" />
                            <span className="truncate">{v.last_error}</span>
                          </div>
                        )}
                      </div>
                      <div className="min-w-0 text-sm text-slate-500 dark:text-slate-400 truncate" title={formatDateTime(v.created_at, locale)}>
                        {formatDateTime(v.created_at, locale)}
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 pr-4">
                        <div className="block truncate" title={v.owner_username || v.owner_id}>{t('admin.resources.userLabel', { value: v.owner_username || v.owner_id || 'unknown' })}</div>
                      </div>
                      <div className="flex gap-2 justify-end shrink-0">
                        <Button
                          variant="danger"
                          className="h-8 px-2"
                          disabled={delVolMut.isPending}
                          onClick={() => requestConfirm(t('volumes.deleteConfirm', { name: v.name || v.id }), () => delVolMut.mutate(v.id))}
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
                      </div>
                      <div className="min-w-0 text-sm">
                        <Badge variant="info">{img.size_mb} MB</Badge>
                      </div>
                      <div className="min-w-0">
                        <Badge variant={statusVariant(img.status)}>{statusLabel(t, img.status)}</Badge>
                        {img.last_error && (
                          <div className="mt-1 flex items-center gap-1 text-xs text-red-600 dark:text-red-400" title={img.last_error}>
                            <AlertTriangle className="h-3 w-3 shrink-0" />
                            <span className="truncate">{img.last_error}</span>
                          </div>
                        )}
                      </div>
                      <div className="min-w-0 text-sm text-slate-500 dark:text-slate-400 truncate" title={formatDateTime(img.created_at, locale)}>
                        {formatDateTime(img.created_at, locale)}
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 pr-4">
                        <div className="block truncate" title={img.owner_username || img.owner_id}>{t('admin.resources.userLabel', { value: img.owner_username || img.owner_id || 'unknown' })}</div>
                      </div>
                      <div className="flex gap-2 justify-end shrink-0">
                        <Button
                          variant="danger"
                          className="h-8 px-2"
                          disabled={delImgMut.isPending}
                          onClick={() => requestConfirm(t('images.deleteConfirm', { name: img.tag }), () => delImgMut.mutate(img.id))}
                        >
                          <Trash2 className="w-4 h-4"/>
                        </Button>
                      </div>
                    </div>
                  ))}

                  {activeTab === 'builds' && buildsData?.items.map((build) => {
                    const canCancel = build.status === 'pending' || build.status === 'running';
                    const canViewLogs = terminalBuildStatuses.has(build.status);
                    const duration = buildDurationSeconds(build.started_at, build.finished_at);
                    return (
                      <div key={build.id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", tableLayouts.adminBuilds.grid)}>
                        <div className="min-w-0 pr-4 pl-2">
                          <div className="font-medium block truncate font-mono" title={build.id}>{build.id}</div>
                        </div>
                        <div className="min-w-0">
                          <Badge variant={statusVariant(build.status)}>{statusLabel(t, build.status)}</Badge>
                        </div>
                        <div className="min-w-0 text-xs font-mono text-slate-500 truncate" title={build.image_id}>{build.image_id}</div>
                        <div className="min-w-0 text-xs font-mono text-slate-500 truncate" title={build.owner_username || build.owner_id}>{t('admin.resources.userLabel', { value: build.owner_username || build.owner_id || 'unknown' })}</div>
                        <div className="min-w-0 text-xs text-slate-500 truncate" title={formatDateTime(build.started_at, locale)}>{formatDateTime(build.started_at, locale)}</div>
                        <div className="min-w-0 text-sm text-slate-600 dark:text-slate-300">
                          {duration !== null ? t('images.seconds', { value: duration }) : '-'}
                        </div>
                        <div className="flex gap-2 justify-end shrink-0">
                          <Button
                            variant="secondary"
                            className="h-8 px-2"
                            disabled={!canViewLogs}
                            title={canViewLogs ? t('images.viewLogs') : t('images.logsAfterFinish')}
                            onClick={() => setViewLogsBuild(build)}
                          >
                            <ScrollText className="w-4 h-4" />
                          </Button>
                          <Button
                            variant="secondary"
                            className="h-8 px-2"
                            disabled={!canCancel || cancelBuildMut.isPending}
                            onClick={() => requestConfirm(t('images.cancelBuildConfirm', { id: build.id }), () => cancelBuildMut.mutate(build.id), t('projects.cancelRequested'), 'secondary')}
                          >
                            <XCircle className="w-4 h-4 text-yellow-500" />
                          </Button>
                          <Button
                            variant="danger"
                            className="h-8 px-2"
                            disabled={canCancel || delBuildMut.isPending}
                            onClick={() => requestConfirm(t('images.deleteBuildConfirm', { id: build.id }), () => delBuildMut.mutate(build.id))}
                          >
                            <Trash2 className="w-4 h-4"/>
                          </Button>
                        </div>
                      </div>
                    );
                  })}

                  {activeTab === 'projects' && projectsData?.items.map((p) => {
                    const isProjectBusy = ['building', 'deploying', 'canceling', 'pending', 'starting', 'stopping', 'deleting'].includes(p.status);
                    const isProjectCanceled = p.status === 'canceled';
                    const canCancelProject = p.status === 'building' || p.status === 'deploying';
                    return (
                    <div key={p.id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", tableLayouts.adminProjects.grid)}>
                      <div className="min-w-0 pr-4 pl-2">
                        <div className="font-medium block truncate" title={p.name}>{p.name}</div>
                        <div className="text-xs text-slate-500 block truncate font-mono">{p.id}</div>
                      </div>
                      <div className="min-w-0">
                        <Badge variant={statusVariant(p.status)}>
                          {statusLabel(t, p.status)}
                        </Badge>
                      </div>
                      <div className="min-w-0 flex items-center">
                        {resourceError(p.error_message || p.last_error) ? (
                          <div className="flex items-center gap-2 text-sm text-red-600 dark:text-red-400 truncate max-w-full cursor-help" title={resourceError(p.error_message || p.last_error)}>
                            <AlertTriangle className="h-3 w-3 shrink-0" />
                            <span className="truncate">{resourceError(p.error_message || p.last_error)}</span>
                          </div>
                        ) : (
                          <span className="text-slate-400 dark:text-slate-500">-</span>
                        )}
                      </div>
                      <div className="min-w-0 text-sm text-slate-500 dark:text-slate-400 truncate" title={formatDateTime(p.created_at, locale)}>
                        {formatDateTime(p.created_at, locale)}
                      </div>
                      <div className="min-w-0 text-xs font-mono text-slate-500 space-y-1 pr-4">
                        <div className="block truncate" title={p.owner_username || p.owner_id}>{t('admin.resources.userLabel', { value: p.owner_username || p.owner_id || 'unknown' })}</div>
                      </div>
                      <div className="flex gap-2 justify-end shrink-0">
                        <Button 
                          variant="secondary" 
                          className="h-8 px-2" 
                          disabled={actionProjMut.isPending || isProjectBusy || isProjectCanceled || p.status === 'failed' || p.status === 'running'}
                          onClick={() => actionProjMut.mutate({id: p.id, action: 'start'})}
                        >
                          <Play className="w-4 h-4 text-green-500"/>
                        </Button>
                        <Button 
                          variant="secondary" 
                          className="h-8 px-2" 
                          disabled={actionProjMut.isPending || isProjectBusy || isProjectCanceled || p.status === 'failed' || p.status === 'stopped'}
                          onClick={() => actionProjMut.mutate({id: p.id, action: 'stop'})}
                        >
                          <Square className="w-4 h-4 text-yellow-500"/>
                        </Button>
                        <Button
                          variant="secondary"
                          className="h-8 px-2"
                          disabled={actionProjMut.isPending || !canCancelProject}
                          onClick={() => requestConfirm(t('projects.cancelConfirm', { name: p.name }), () => actionProjMut.mutate({ id: p.id, action: 'cancel' }), t('projects.cancelRequested'), 'secondary')}
                        >
                          <XCircle className="w-4 h-4 text-orange-500"/>
                        </Button>
                        <Button
                          variant="danger"
                          className="h-8 px-2"
                          disabled={actionProjMut.isPending || isProjectBusy}
                          onClick={() => requestConfirm(t('projects.deleteConfirm', { name: p.name }), () => actionProjMut.mutate({ id: p.id, action: 'delete' }))}
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

      <ConfirmDialog
        isOpen={!!confirmAction}
        title={t('confirm.title')}
        message={confirmAction?.message || ''}
        confirmLabel={confirmAction?.confirmLabel || t('common.delete')}
        variant={confirmAction?.variant || 'danger'}
        isLoading={isMutating}
        onCancel={() => setConfirmAction(null)}
        onConfirm={() => {
          confirmAction?.run();
          setConfirmAction(null);
        }}
      />

    </div>
  );
}
