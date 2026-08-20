import { useState } from 'react';
import { Box, RefreshCcw } from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { ContainerRow } from '@/features/containers/components/ContainerRow';
import { useContainers } from '@/features/containers/hooks';
import type { ContainerData } from '@/features/containers/types';
import type { ProjectData } from '../types';
import { ProjectRow } from './ProjectRow';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';

interface ProjectWithContainersRowProps {
  project: ProjectData;
  onExpose: (container: ContainerData) => void;
  onViewLogs: (container: ContainerData) => void;
  onOpenTerminal: (container: ContainerData) => void;
}

export function ProjectWithContainersRow({
  project,
  onExpose,
  onViewLogs,
  onOpenTerminal,
}: ProjectWithContainersRowProps) {
  const t = useT();
  const [isExpanded, setIsExpanded] = useState(true);
  const { data, isLoading, isError, error, refetch, isFetching } = useContainers(1, 100, project.id, isExpanded);
  const containers = data?.items || [];

  return (
    <div className="border-b border-white/20 dark:border-slate-700/50 last:border-0">
      <ProjectRow
        project={project}
        isExpanded={isExpanded}
        onToggleExpanded={() => setIsExpanded((value) => !value)}
      />

      {isExpanded && (
        <div className="bg-slate-50/40 dark:bg-slate-950/20">
          {isLoading ? (
            <div className="space-y-2 px-6 py-4">
              {[...Array(2)].map((_, i) => (
                <div key={i} className="flex items-center gap-4">
                  <div className="h-9 w-9 shrink-0 rounded-xl bg-slate-200/60 dark:bg-slate-700/60 animate-pulse" />
                  <div className="min-w-0 flex-1 space-y-2">
                    <div className="h-4 w-1/3 rounded bg-slate-200/60 dark:bg-slate-700/60 animate-pulse" />
                    <div className="h-3 w-1/2 rounded bg-slate-200/60 dark:bg-slate-700/60 animate-pulse" />
                  </div>
                </div>
              ))}
            </div>
          ) : isError ? (
            <div className="flex items-center justify-between gap-4 px-6 py-4 text-sm text-red-600 dark:text-red-400">
              <span className="truncate">{getApiErrorMessage(error, t('containers.loadFailed'), t).message}</span>
              <Button
                type="button"
                variant="secondary"
                onClick={() => refetch()}
                isLoading={isFetching}
                className="shrink-0 px-3 py-2"
                title={t('common.refresh')}
              >
                <RefreshCcw className="h-4 w-4" />
              </Button>
            </div>
          ) : containers.length > 0 ? (
            <div className="flex flex-col">
              {containers.map((container) => (
                <ContainerRow
                  key={container.id}
                  container={container}
                  onExpose={onExpose}
                  onViewLogs={onViewLogs}
                  onOpenTerminal={onOpenTerminal}
                />
              ))}
            </div>
          ) : (
            <div className="flex items-center gap-3 px-6 py-4 text-sm text-slate-500 dark:text-slate-400">
              <Box className="h-4 w-4 shrink-0" />
              <span>{t('projects.noContainers')}</span>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
