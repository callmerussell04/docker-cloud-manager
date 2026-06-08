import { useLocation, useParams, Link } from 'react-router-dom';
import { Activity, ArrowLeft, Cpu, HardDrive, Network, Box, Terminal, ScrollText } from 'lucide-react';
import { useState } from 'react';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Badge } from '@/components/ui/Badge';
import { formatBytes } from '@/lib/utils';
import type { ContainerData } from '@/features/containers/types';
import { ContainerLogsModal } from '@/features/containers/components/ContainerLogsModal';
import { ContainerTerminalModal } from '@/features/containers/components/ContainerTerminalModal';
import { ContainerTTLTimer } from '@/features/containers/components/ContainerTTLTimer';
import { statusLabel, useT } from '@/lib/i18n';
import { useContainerDetailsStats } from '@/features/containers/hooks';

export function ContainerDetailsPage() {
  const t = useT();
  const { id } = useParams<{ id: string }>();
  const location = useLocation();
  const isAdminRoute = location.pathname.startsWith('/admin');
  
  const container = location.state?.container as ContainerData | undefined;
  const streamContainer: ContainerData | null = id ? {
    id,
    name: container?.name || id,
    image_tag: container?.image_tag || '',
    internal_port: container?.internal_port || 0,
    domain_prefix: container?.domain_prefix || '',
    status: container?.status || 'unknown',
    desired_status: container?.desired_status,
    last_error: container?.last_error || '',
    last_exit_code: container?.last_exit_code,
    ttl_deadline: container?.ttl_deadline,
    created_at: container?.created_at || 0,
  } : null;

  const [logsContainer, setLogsContainer] = useState<ContainerData | null>(null);
  const [terminalContainer, setTerminalContainer] = useState<ContainerData | null>(null);

  const { data: stats, isLoading } = useContainerDetailsStats(id, isAdminRoute, container);

  const getStatusBadge = (status?: string) => {
    switch (status) {
      case 'running': return <Badge variant="success">{statusLabel(t, status)}</Badge>;
      case 'exited': return <Badge variant="default">{statusLabel(t, status)}</Badge>;
      case 'creating': return <Badge variant="warning">{statusLabel(t, status)}</Badge>;
      case 'starting': return <Badge variant="info">{statusLabel(t, status)}</Badge>;
      case 'stopping': return <Badge variant="warning">{statusLabel(t, status)}</Badge>;
      case 'exposing': return <Badge variant="info">{statusLabel(t, status)}</Badge>;
      case 'deleting': return <Badge variant="warning">{statusLabel(t, status)}</Badge>;
      case 'missing': return <Badge variant="error">{statusLabel(t, status)}</Badge>;
      case 'reconciling': return <Badge variant="warning">{statusLabel(t, status)}</Badge>;
      case 'error': return <Badge variant="error">{statusLabel(t, status)}</Badge>;
      default: return <Badge variant="info">{statusLabel(t, status)}</Badge>;
    }
  };

  const memPercent = stats?.memory_limit_bytes 
    ? (stats.memory_usage_bytes / stats.memory_limit_bytes) * 100 
    : 0;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-4">
          <Link 
            to={isAdminRoute ? '/admin/resources' : '/containers'} 
            className="p-2 rounded-xl bg-white/40 dark:bg-slate-800/40 hover:bg-white/60 dark:hover:bg-slate-700/50 border border-white/50 dark:border-slate-700/50 transition-colors"
          >
            <ArrowLeft className="w-5 h-5" />
          </Link>
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-3xl font-bold tracking-tight">{container?.name || id}</h1>
              {getStatusBadge(container?.status)}
            </div>
            {container && (
              <ContainerTTLTimer status={container.status} ttlDeadline={container.ttl_deadline} className="mt-2" />
            )}
            {container && (
              <div className="flex items-center gap-2 mt-1 text-sm text-slate-500 dark:text-slate-400">
                <Box className="w-4 h-4" />
                <span>{container.image_tag}</span>
              </div>
            )}
          </div>
        </div>

        {streamContainer && (
          <div className="flex items-center gap-2">
            <button
              onClick={() => setTerminalContainer(streamContainer)}
              disabled={!!container && container.status !== 'running'}
              className="flex items-center gap-2 px-4 py-2.5 rounded-xl bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:hover:bg-slate-700 transition-colors disabled:opacity-50"
            >
              <Terminal className="w-4 h-4" />
              <span className="hidden sm:inline font-medium text-sm">{t('containers.terminal')}</span>
            </button>
            <button
              onClick={() => setLogsContainer(streamContainer)}
              className="flex items-center gap-2 px-4 py-2.5 rounded-xl bg-slate-100 text-slate-700 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:hover:bg-slate-700 transition-colors"
            >
              <ScrollText className="w-4 h-4" />
              <span className="hidden sm:inline font-medium text-sm">{t('containers.logs')}</span>
            </button>
          </div>
        )}
      </div>

      {container?.status !== 'running' && (
        <div className="bg-yellow-50 dark:bg-yellow-900/20 text-yellow-800 dark:text-yellow-300 p-4 rounded-xl border border-yellow-200 dark:border-yellow-900/50 text-sm flex items-center gap-2">
          <Activity className="w-4 h-4" />
          {t('containers.statsOnlyRunning')}
        </div>
      )}

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <Card>
          <CardHeader className="pb-4">
            <CardTitle className="text-lg flex items-center gap-2">
              <Cpu className="w-5 h-5 text-indigo-500" />
              {t('containers.cpu')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="h-16 animate-pulse bg-slate-200 dark:bg-slate-800 rounded-lg" />
            ) : (
              <div className="space-y-4">
                <div className="flex justify-between items-end">
                  <span className="text-3xl font-semibold">{stats?.cpu_percentage?.toFixed(2) || '0.00'}%</span>
                </div>
                <div className="w-full bg-slate-100 dark:bg-slate-800 rounded-full h-3 border border-slate-200 dark:border-slate-700 overflow-hidden">
                  <div 
                    className="bg-indigo-500 h-full rounded-full transition-all duration-500" 
                    style={{ width: `${Math.min(stats?.cpu_percentage || 0, 100)}%` }} 
                  />
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-4">
            <CardTitle className="text-lg flex items-center gap-2">
              <HardDrive className="w-5 h-5 text-emerald-500" />
              {t('containers.memory')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="h-16 animate-pulse bg-slate-200 dark:bg-slate-800 rounded-lg" />
            ) : (
              <div className="space-y-4">
                <div className="flex justify-between items-end">
                  <span className="text-3xl font-semibold">{formatBytes(stats?.memory_usage_bytes || 0)}</span>
                  <span className="text-sm text-slate-500 dark:text-slate-400 mb-1">/ {formatBytes(stats?.memory_limit_bytes || 0)}</span>
                </div>
                <div className="w-full bg-slate-100 dark:bg-slate-800 rounded-full h-3 border border-slate-200 dark:border-slate-700 overflow-hidden">
                  <div 
                    className="bg-emerald-500 h-full rounded-full transition-all duration-500" 
                    style={{ width: `${Math.min(memPercent, 100)}%` }} 
                  />
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-4">
            <CardTitle className="text-lg flex items-center gap-2">
              <Network className="w-5 h-5 text-blue-500" />
              {t('containers.network')}
            </CardTitle>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <div className="h-16 animate-pulse bg-slate-200 dark:bg-slate-800 rounded-lg" />
            ) : (
              <div className="space-y-4">
                <div className="flex items-center justify-between p-3 bg-slate-50 dark:bg-slate-800/50 rounded-xl border border-slate-100 dark:border-slate-700/50">
                  <span className="text-sm text-slate-500 dark:text-slate-400">{t('containers.rx')}</span>
                  <span className="font-semibold">{formatBytes(stats?.network_rx_bytes || 0)}</span>
                </div>
                <div className="flex items-center justify-between p-3 bg-slate-50 dark:bg-slate-800/50 rounded-xl border border-slate-100 dark:border-slate-700/50">
                  <span className="text-sm text-slate-500 dark:text-slate-400">{t('containers.tx')}</span>
                  <span className="font-semibold">{formatBytes(stats?.network_tx_bytes || 0)}</span>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <ContainerLogsModal 
        container={logsContainer}
        isAdmin={isAdminRoute}
        onClose={() => setLogsContainer(null)}
      />

      <ContainerTerminalModal
        container={terminalContainer}
        isAdmin={isAdminRoute}
        onClose={() => setTerminalContainer(null)}
      />
    </div>
  );
}
