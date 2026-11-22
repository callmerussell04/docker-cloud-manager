import { useState, type ComponentType } from 'react';
import { useIsFetching, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { Box, Disc, Hammer, HardDrive, Layers, RefreshCcw, ShieldAlert } from 'lucide-react';

import { Button } from '@/shared/ui/Button';
import { TabSwitcher } from '@/shared/ui/TabSwitcher';
import { useT } from '@/shared/i18n';
import { containerKeys } from '@/features/containers/queries';
import { volumeKeys } from '@/features/volumes/queries';
import { buildKeys, imageKeys } from '@/features/images/queries';
import { projectKeys } from '@/features/projects/queries';
import { ContainersTab } from '../components/ContainersTab';
import { VolumesTab } from '../components/VolumesTab';
import { ImagesTab } from '../components/ImagesTab';
import { BuildsTab } from '../components/BuildsTab';
import { ProjectsTab } from '../components/ProjectsTab';

type Tab = 'containers' | 'volumes' | 'images' | 'builds' | 'projects';

const tabs: Record<Tab, { listKey: QueryKey; Component: ComponentType }> = {
  containers: { listKey: containerKeys.lists('all'), Component: ContainersTab },
  volumes: { listKey: volumeKeys.lists('all'), Component: VolumesTab },
  images: { listKey: imageKeys.lists('all'), Component: ImagesTab },
  builds: { listKey: buildKeys.lists('all'), Component: BuildsTab },
  projects: { listKey: projectKeys.lists('all'), Component: ProjectsTab },
};

export function AllResourcesPage() {
  const t = useT();
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<Tab>('containers');
  const { listKey, Component: ActiveTab } = tabs[activeTab];
  const isFetching = useIsFetching({ queryKey: listKey }) > 0;

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

        <Button variant="secondary" onClick={() => queryClient.invalidateQueries({ queryKey: listKey })} isLoading={isFetching} className="px-3">
          <RefreshCcw className="w-4 h-4 mr-2" /> {t('common.refresh')}
        </Button>
      </div>

      <TabSwitcher
        activeTab={activeTab}
        onChange={setActiveTab}
        items={[
          { id: 'containers', label: t('nav.containers'), icon: Box },
          { id: 'volumes', label: t('nav.volumes'), icon: HardDrive },
          { id: 'images', label: t('nav.images'), icon: Disc },
          { id: 'builds', label: t('images.buildHistory'), icon: Hammer },
          { id: 'projects', label: t('projects.title'), icon: Layers },
        ]}
      />

      <ActiveTab key={activeTab} />
    </div>
  );
}
