import { useMemo } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';

import { useT } from '@/shared/i18n';
import { useDebouncedValue, useListState } from '@/shared/lib/hooks';
import { readListPage } from '@/shared/lib/listState';
import { ContainerRow } from '@/features/containers/components/ContainerRow';
import { containerListLayout } from '@/features/containers/components/listLayout';
import {
  containerFilters,
  containerListParams,
  containerPagePath,
  containerSortKeys,
  defaultContainerSort,
  type ContainerData,
  type ConsoleView,
} from '@/features/containers/model';
import { useContainers } from '@/features/containers/queries';
import { VolumeRow } from '@/features/volumes/components/VolumeRow';
import { volumeListLayout } from '@/features/volumes/components/listLayout';
import { defaultVolumeSort, matchesVolumeSearch, sortVolumes, volumeSortKeys } from '@/features/volumes/model';
import { useVolumes } from '@/features/volumes/queries';
import { BuildRow } from '@/features/images/components/BuildRow';
import { ImageRow } from '@/features/images/components/ImageRow';
import { buildListLayout, imageListLayout } from '@/features/images/components/listLayout';
import {
  buildSortKeys,
  defaultBuildSort,
  defaultImageSort,
  imageSortKeys,
  matchesBuildSearch,
  matchesImageSearch,
  sortBuilds,
  sortImages,
} from '@/features/images/model';
import { useBuilds, useImages } from '@/features/images/queries';
import { ProjectRow } from '@/features/projects/components/ProjectRow';
import { projectListLayout } from '@/features/projects/components/listLayout';
import {
  defaultProjectSort,
  isProjectBusy,
  matchesProjectSearch,
  projectSortKeys,
  servicesByProject,
  sortProjects,
} from '@/features/projects/model';
import { useProjects } from '@/features/projects/queries';
import { useAdminUsers } from '../../users/queries';
import { ResourceList, resourcePageSize, type ResourceView } from '../components/ResourceList';
import { readOwnerFilter, readResourceTab, resourceListSpec, withOwnerFilter, withResourceTab } from '../model';

// AllResourcesPage is every user's containers, volumes, images, builds and
// projects behind toolbar pills, each list with an Owner column and the
// owner filter beside the pills; the tab and the owner are in the URL.
export function AllResourcesPage() {
  const [params, setParams] = useSearchParams();
  const tab = readResourceTab(params);
  // The accounts the owner filter offers, beside the owners on the page.
  const users = useAdminUsers(1, 100);
  const accounts = useMemo(() => users.data?.items ?? [], [users.data]);

  const view: ResourceView = {
    tab,
    onTab: (next) => setParams((current) => withResourceTab(current, next), { replace: true }),
    owner: readOwnerFilter(params),
    onOwner: (next) => setParams((current) => withOwnerFilter(current, next), { replace: true }),
    accounts,
  };

  switch (tab) {
    case 'containers':
      return <ContainersTab key={tab} view={view} />;
    case 'volumes':
      return <VolumesTab key={tab} view={view} />;
    case 'images':
      return <ImagesTab key={tab} view={view} />;
    case 'builds':
      return <BuildsTab key={tab} view={view} />;
    case 'projects':
      return <ProjectsTab key={tab} view={view} />;
  }
}

const containerListSpec = resourceListSpec(containerSortKeys, defaultContainerSort, containerFilters);
const volumeListSpec = resourceListSpec(volumeSortKeys, defaultVolumeSort);
const imageListSpec = resourceListSpec(imageSortKeys, defaultImageSort);
const buildListSpec = resourceListSpec(buildSortKeys, defaultBuildSort);
const projectListSpec = resourceListSpec(projectSortKeys, defaultProjectSort);

function ContainersTab({ view }: { view: ResourceView }) {
  const t = useT();
  const navigate = useNavigate();
  // The server filters, sorts, searches and counts every user's containers.
  // It cannot sort by owner (usernames live in SSO), so that column sorts the
  // page it answers in its default order.
  const [listView] = useListState(containerListSpec);
  const search = useDebouncedValue(listView.search);
  const sort = listView.sort.key === 'owner' ? defaultContainerSort : { key: listView.sort.key, ascending: listView.sort.ascending };
  const list = containerListParams({ filter: listView.filter, sort, search });
  const query = useContainers('all', listView.page, resourcePageSize, { list });
  const counts = query.data?.status_counts;
  // Compose projects name the tag beside their containers.
  const projects = useProjects('all', 1, 100);
  const projectNames = useMemo(() => new Map((projects.data?.items ?? []).map((project) => [project.id, project.name])), [projects.data]);
  const open = (container: ContainerData, view?: ConsoleView) =>
    navigate(containerPagePath('/admin/containers', container.id, view), { state: { container } });

  return (
    <ResourceList
      view={view}
      label={t('admin.resources.tab.containers')}
      columns={[
        { key: 'name', label: t('containers.name') },
        { key: 'status', label: t('common.status') },
        { key: 'address', label: t('containers.column.address') },
        { key: 'memory', label: t('containers.column.memory') },
        { key: 'created', label: t('containers.column.created'), end: true },
      ]}
      template={containerListLayout.owned}
      loadingColumns={[1.5, 0.8, 1.2, 1, 0.4, 0.5]}
      spec={containerListSpec}
      count={counts?.all}
      statusFilter={{
        label: t('admin.resources.statusFilter'),
        options: containerFilters.map((filter) => ({ id: filter, label: t(`containers.filter.${filter}`), count: counts?.[filter] })),
        alert: counts ? { count: counts.attention, label: t('admin.resources.attentionCount', { count: counts.attention }) } : undefined,
        emptyTitle: t('containers.filterEmpty'),
        emptyDescription: t('admin.resources.statusEmptyDescription'),
        showAll: t('containers.showAll'),
      }}
      nameOf={(container) => container.name}
      searchLabel={t('containers.searchLabel')}
      loadFailed={t('containers.loadFailed')}
      emptyTitle={t('admin.resources.empty.containers')}
      query={query}
      onRefresh={() => void query.refetch()}
      row={(container, owner) => (
        <ContainerRow
          key={container.id}
          container={container}
          scope="all"
          owner={owner}
          project={container.project_id ? (projectNames.get(container.project_id) ?? t('containers.composeTag')) : undefined}
          onOpen={open}
        />
      )}
    />
  );
}

function VolumesTab({ view }: { view: ResourceView }) {
  const t = useT();
  const [params] = useSearchParams();
  const query = useVolumes('all', readListPage(params), resourcePageSize);
  return (
    <ResourceList
      view={view}
      label={t('admin.resources.tab.volumes')}
      columns={[
        { key: 'name', label: t('volumes.column.volume') },
        { key: 'status', label: t('common.status') },
        { key: 'size', label: t('volumes.column.size') },
        { key: 'usedBy', label: t('volumes.column.usedBy') },
        { key: 'created', label: t('volumes.column.created'), end: true },
      ]}
      template={volumeListLayout.owned}
      loadingColumns={[1.3, 0.8, 0.9, 0.9, 1, 0.5]}
      spec={volumeListSpec}
      sort={sortVolumes}
      matches={matchesVolumeSearch}
      nameOf={(volume) => volume.name}
      searchLabel={t('volumes.searchLabel')}
      loadFailed={t('volumes.loadFailed')}
      emptyTitle={t('admin.resources.empty.volumes')}
      query={query}
      onRefresh={() => void query.refetch()}
      row={(volume, owner) => <VolumeRow key={volume.id} volume={volume} scope="all" owner={owner} />}
    />
  );
}

function ImagesTab({ view }: { view: ResourceView }) {
  const t = useT();
  const [params] = useSearchParams();
  const query = useImages('all', readListPage(params), resourcePageSize);
  return (
    <ResourceList
      view={view}
      label={t('admin.resources.tab.images')}
      columns={[
        { key: 'name', label: t('images.column.image') },
        { key: 'status', label: t('common.status') },
        { key: 'usedBy', label: t('images.column.usedBy') },
        { key: 'size', label: t('images.column.size') },
        { key: 'created', label: t('images.column.created'), end: true },
      ]}
      template={imageListLayout.owned}
      loadingColumns={[1.4, 0.8, 0.9, 1, 0.6, 0.5]}
      spec={imageListSpec}
      sort={sortImages}
      matches={matchesImageSearch}
      nameOf={(image) => image.tag}
      searchLabel={t('images.searchLabel')}
      loadFailed={t('images.loadFailed')}
      emptyTitle={t('admin.resources.empty.images')}
      query={query}
      onRefresh={() => void query.refetch()}
      row={(image, owner) => <ImageRow key={image.id} image={image} scope="all" owner={owner} />}
    />
  );
}

function BuildsTab({ view }: { view: ResourceView }) {
  const t = useT();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const query = useBuilds('all', readListPage(params), resourcePageSize);
  return (
    <ResourceList
      view={view}
      label={t('admin.resources.tab.builds')}
      columns={[
        { key: 'tag', label: t('images.column.build') },
        { key: 'purpose', label: t('images.column.for') },
        { key: 'status', label: t('common.status') },
        { key: 'duration', label: t('images.column.took') },
        { key: 'queued', label: t('images.column.queued'), end: true },
      ]}
      template={buildListLayout.owned}
      loadingColumns={[1.2, 0.8, 1.2, 1, 0.6, 0.5]}
      spec={buildListSpec}
      // Sorting by how long a build took counts a running one up to now.
      sort={(rows, sort) => sortBuilds(rows, sort, Math.floor(Date.now() / 1000))}
      matches={matchesBuildSearch}
      nameOf={(build) => build.tag || build.id}
      searchLabel={t('images.searchBuildsLabel')}
      loadFailed={t('images.loadBuildsFailed')}
      emptyTitle={t('admin.resources.empty.builds')}
      query={query}
      onRefresh={() => void query.refetch()}
      row={(build, owner) => (
        <BuildRow
          key={build.id}
          build={build}
          scope="all"
          owner={owner}
          onOpen={() => navigate(`/admin/builds/${build.id}`, { state: { build } })}
        />
      )}
    />
  );
}

function ProjectsTab({ view }: { view: ResourceView }) {
  const t = useT();
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const query = useProjects('all', readListPage(params), resourcePageSize);
  const projects = useMemo(() => query.data?.items ?? [], [query.data]);
  // A project's services are its containers among every user's first 100,
  // refreshing while a project deploys, starts or stops.
  const containers = useContainers('all', 1, 100, { polling: projects.some((project) => isProjectBusy(project.status)) });
  const services = useMemo(() => servicesByProject(containers.data?.items ?? []), [containers.data]);
  // Each item carries its project's owner, for the Owner column and filter.
  const items = projects.map((project) => ({
    project,
    services: services.get(project.id) ?? [],
    owner_id: project.owner_id,
    owner_username: project.owner_username,
  }));
  const itemsQuery = { ...query, data: query.data && { items, total_count: query.data.total_count } };

  return (
    <ResourceList
      view={view}
      label={t('admin.resources.tab.projects')}
      columns={[
        { key: 'name', label: t('projects.project') },
        { key: 'services', label: t('projects.services') },
        { key: 'address', label: t('projects.column.address') },
        { key: 'memory', label: t('projects.column.memory') },
        { key: 'created', label: t('projects.column.created'), end: true },
      ]}
      template={projectListLayout.owned}
      loadingColumns={[1.4, 0.8, 1.3, 1, 0.5, 0.5]}
      spec={projectListSpec}
      sort={sortProjects}
      matches={matchesProjectSearch}
      nameOf={(item) => item.project.name}
      searchLabel={t('projects.searchLabel')}
      loadFailed={t('projects.loadFailed')}
      emptyTitle={t('admin.resources.empty.projects')}
      query={itemsQuery}
      onRefresh={() => {
        void query.refetch();
        void containers.refetch();
      }}
      refreshing={containers.isFetching}
      row={(item, owner) => (
        <ProjectRow
          key={item.project.id}
          item={item}
          scope="all"
          owner={owner}
          onOpen={() => navigate(`/admin/projects/${item.project.id}`, { state: { project: item.project } })}
        />
      )}
    />
  );
}
