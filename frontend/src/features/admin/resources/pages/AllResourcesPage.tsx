import { useState, type ComponentType } from 'react';
import { useIsFetching, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { Box, Disc, Hammer, HardDrive, Layers, RefreshCcw } from 'lucide-react';

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
import { Page } from '@/shared/ui/Page';
import { Toolbar, ToolbarButton } from '@/shared/ui/Toolbar';

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
    <Page
      toolbar={
        <Toolbar
          title={t('admin.resources.title')}
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => queryClient.invalidateQueries({ queryKey: listKey })} />}
        />
      }
    >

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
    </Page>
  );
}
