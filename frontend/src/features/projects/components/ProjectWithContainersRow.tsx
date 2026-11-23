import { useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Box } from 'lucide-react';

import { ErrorState } from '@/shared/ui/ErrorState';
import { List } from '@/shared/ui/List';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { ContainerRow } from '@/features/containers/components/ContainerRow';
import { useContainers } from '@/features/containers/queries';
import { containerPagePath, type ContainerData } from '@/features/containers/model';
import { isProjectBusy, type ProjectData } from '../model';
import { ProjectRow } from './ProjectRow';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';

interface ProjectWithContainersRowProps {
  project: ProjectData;
  onExpose: (container: ContainerData) => void;
}

export function ProjectWithContainersRow({
  project,
  onExpose,
}: ProjectWithContainersRowProps) {
  const t = useT();
  const navigate = useNavigate();
  const [isExpanded, setIsExpanded] = useState(true);
  // A deploying, starting or stopping project changes its containers, so the
  // list keeps refreshing even while it is still empty.
  const { data, isLoading, isError, error, refetch, isFetching } = useContainers('own', 1, 100, {
    projectId: project.id,
    enabled: isExpanded,
    polling: isProjectBusy(project.status),
  });

  // Reaching a new status ends that polling, so read the result once more.
  const lastStatus = useRef(project.status);
  useEffect(() => {
    if (lastStatus.current === project.status) return;
    lastStatus.current = project.status;
    if (isExpanded) void refetch();
  }, [project.status, isExpanded, refetch]);
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
            <LoadingRows rows={2} className="px-4 py-2" />
          ) : isError ? (
            <div className="px-4">
              <ErrorState
                title={t('containers.loadFailed')}
                message={failureReason(error, t)}
                onRetry={() => refetch()}
                isRetrying={isFetching}
              />
            </div>
          ) : containers.length > 0 ? (
            <List label={project.name}>
              {containers.map((container) => (
                <ContainerRow
                  key={container.id}
                  container={container}
                  onOpen={(opened, view) => navigate(containerPagePath('/containers', opened.id, view), { state: { container: opened } })}
                  onExpose={onExpose}
                />
              ))}
            </List>
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
