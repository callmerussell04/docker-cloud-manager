import { useEffect, useState, type ReactNode } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Plus, RefreshCcw } from 'lucide-react';

import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { createDialogFlag, createWithImageParam, useListState, useNowSeconds, useSearchFlag } from '@/shared/lib/hooks';
import { toggleSort, type ListStateSpec } from '@/shared/lib/listState';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorState } from '@/shared/ui/ErrorState';
import { List, ListHead, type ListColumn } from '@/shared/ui/List';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { Page } from '@/shared/ui/Page';
import { Pagination } from '@/shared/ui/Pagination';
import { Pills } from '@/shared/ui/Pills';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Toolbar, ToolbarButton, ToolbarPrimary, ToolbarSearch } from '@/shared/ui/Toolbar';
import { BuildImageDialog } from '../components/BuildImageDialog';
import { BuildRow } from '../components/BuildRow';
import { ImageRow } from '../components/ImageRow';
import { buildListLayout, imageListLayout } from '../components/listLayout';
import {
  activeBuildCount,
  buildAgainFrom,
  buildSortKeys,
  defaultBuildSort,
  defaultImageSort,
  imageSortKeys,
  matchesBuildSearch,
  matchesImageSearch,
  sortBuilds,
  sortImages,
  type BuildAgain,
  type BuildData,
  type BuildSortKey,
  type ImageData,
  type ImageSortKey,
} from '../model';
import { useBuildAvailability, useBuilds, useImages } from '../queries';

// The page's two lists, switched by the toolbar's pills and kept in the URL
// as ?tab=builds (Images is the default and left out).
type Tab = 'images' | 'builds';
const tabParam = 'tab';

// Neither list has a filter; its one filter value keeps the URL state shape
// every list shares.
const imageListSpec: ListStateSpec<'all', ImageSortKey> = { filters: ['all'], sortKeys: imageSortKeys, defaultSort: defaultImageSort };
const buildListSpec: ListStateSpec<'all', BuildSortKey> = { filters: ['all'], sortKeys: buildSortKeys, defaultSort: defaultBuildSort };

// One page holds a user's whole list in the usual case; the sort and the
// search apply to the page shown.
const pageSize = 100;

export function ImagesPage() {
  const t = useT();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const tab: Tab = params.get(tabParam) === 'builds' ? 'builds' : 'images';
  const [isBuildOpen, setIsBuildOpen] = useSearchFlag(createDialogFlag);
  const [buildAgain, setBuildAgain] = useState<BuildAgain | undefined>();

  const { data: availability } = useBuildAvailability();
  const buildsOff = availability?.enabled === false;
  // A link that asks for the build dialog (?new) while builds are off opens
  // nothing, so the flag is dropped.
  useEffect(() => {
    if (isBuildOpen && buildsOff) setIsBuildOpen(false);
  }, [isBuildOpen, buildsOff, setIsBuildOpen]);

  // The builds' first page counts the active ones for the pill and gives
  // Build again its source, so it is read on both tabs whatever page the
  // Builds list shows; it polls while a build is active.
  const firstBuilds = useBuilds('own', 1, pageSize);
  const activeBuilds = activeBuildCount(firstBuilds.data?.items ?? []);
  const buildsQuery = useBuilds('own', tab === 'builds' ? readPage(params) : 1, pageSize, { enabled: tab === 'builds' });

  // Switching lists starts the other one fresh: its own sort, page and
  // search.
  const changeTab = (next: Tab) => setParams(() => {
    const updated = new URLSearchParams();
    if (next === 'builds') updated.set(tabParam, 'builds');
    return updated;
  }, { replace: true });

  const openBuild = (initial?: BuildAgain) => {
    setBuildAgain(initial);
    setIsBuildOpen(true);
  };

  const runImage = (image: ImageData) => {
    const search = new URLSearchParams();
    search.set(createDialogFlag, '');
    search.set(createWithImageParam, image.tag);
    navigate(`/containers?${search.toString()}`);
  };

  const openBuildPage = (build: BuildData) => navigate(`/images/builds/${build.id}`, { state: { build } });

  const pills = (
    <Pills
      label={t('images.tabsLabel')}
      value={tab}
      onChange={changeTab}
      items={[
        { id: 'images', label: t('images.tab.images') },
        { id: 'builds', label: t('images.tab.builds'), count: activeBuilds > 0 ? activeBuilds : undefined },
      ]}
    />
  );

  const primary = (
    <ToolbarPrimary
      icon={Plus}
      onClick={() => openBuild()}
      disabled={buildsOff}
      {...(buildsOff ? { title: t('images.buildUnavailable') } : {})}
      label={t('images.buildImage')}
    />
  );

  return (
    <>
      {tab === 'images' ? (
        <ImagesList
          pills={pills}
          primary={primary}
          onRun={runImage}
          onBuildAgain={buildsOff ? undefined : (image) => openBuild(buildAgainFrom(image, firstBuilds.data?.items ?? []))}
          onBuild={buildsOff ? undefined : () => openBuild()}
        />
      ) : (
        <BuildsList pills={pills} primary={primary} query={buildsQuery} onOpen={openBuildPage} onBuild={buildsOff ? undefined : () => openBuild()} />
      )}
      <BuildImageDialog
        isOpen={isBuildOpen && !buildsOff}
        initial={buildAgain}
        onClose={() => {
          setIsBuildOpen(false);
          setBuildAgain(undefined);
        }}
        onBuilt={(id) => navigate(`/images/builds/${id}`)}
      />
    </>
  );
}

function readPage(params: URLSearchParams) {
  const page = Number(params.get('page'));
  return Number.isInteger(page) && page >= 1 ? page : 1;
}

interface ImagesListProps {
  pills: ReactNode;
  primary: ReactNode;
  onRun: (image: ImageData) => void;
  onBuildAgain?: (image: ImageData) => void;
  onBuild?: () => void;
}

function ImagesList({ pills, primary, onRun, onBuildAgain, onBuild }: ImagesListProps) {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const [view, changeView] = useListState(imageListSpec);
  const { data, isLoading, isError, error, refetch, isFetching } = useImages('own', view.page, pageSize);
  const images = data?.items ?? [];
  const shown = sortImages(images.filter((image) => matchesImageSearch(image, view.search)), view.sort);

  const columns: ListColumn<ImageSortKey>[] = [
    { key: 'name', label: t('images.column.image') },
    { key: 'status', label: t('common.status') },
    { key: 'usedBy', label: t('images.column.usedBy') },
    { key: 'size', label: t('images.column.size') },
    { key: 'created', label: t('images.column.created'), end: true },
  ];

  const content = () => {
    if (isLoading) return <LoadingRows columns={compact ? [1] : [1.4, 0.9, 1, 0.6, 0.5]} />;
    if (isError) return <ErrorState title={t('images.loadFailed')} message={failureReason(error, t)} onRetry={() => refetch()} isRetrying={isFetching} />;
    if (images.length === 0) {
      return (
        <EmptyState
          title={t('images.noImages')}
          description={t('images.noImagesDescription')}
          action={onBuild ? { label: t('images.buildFirst'), icon: Plus, onClick: onBuild } : undefined}
        />
      );
    }
    if (shown.length === 0) return <NoMatchesState query={view.search} onClear={() => changeView({ search: '' })} />;
    return (
      <List label={t('images.tab.images')}>
        {shown.map((image) => <ImageRow key={image.id} image={image} onRun={onRun} onBuildAgain={onBuildAgain} />)}
      </List>
    );
  };

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('images.title')}
          count={data?.total_count}
          pills={<>{pills}<ToolbarSearch label={t('images.searchLabel')} value={view.search} onChange={(search) => changeView({ search })} /></>}
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => refetch()} />}
          primary={primary}
        />
      }
      head={(isLoading || shown.length > 0) && (
        <ListHead columns={columns} template={imageListLayout.wide} sort={view.sort} onSort={(key) => changeView({ sort: toggleSort(view.sort, key) })} />
      )}
      className="space-y-2"
    >
      {content()}
      {data && <Pagination currentPage={view.page} pageSize={pageSize} totalItems={data.total_count} onPageChange={(page) => changeView({ page })} />}
    </Page>
  );
}

interface BuildsListProps {
  pills: ReactNode;
  primary: ReactNode;
  query: ReturnType<typeof useBuilds<'own'>>;
  onOpen: (build: BuildData) => void;
  onBuild?: () => void;
}

function BuildsList({ pills, primary, query, onOpen, onBuild }: BuildsListProps) {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const [view, changeView] = useListState(buildListSpec);
  // Sorting by how long a build has taken counts a running one up to now.
  const now = useNowSeconds(10_000);
  const { data, isLoading, isError, error, refetch, isFetching } = query;
  const builds = data?.items ?? [];
  const shown = sortBuilds(builds.filter((build) => matchesBuildSearch(build, view.search)), view.sort, now);

  const columns: ListColumn<BuildSortKey>[] = [
    { key: 'tag', label: t('images.column.build') },
    { key: 'purpose', label: t('images.column.for') },
    { key: 'status', label: t('common.status') },
    { key: 'duration', label: t('images.column.took') },
    { key: 'queued', label: t('images.column.queued'), end: true },
  ];

  const content = () => {
    if (isLoading) return <LoadingRows columns={compact ? [1] : [1.2, 1.2, 1, 0.6, 0.5]} />;
    if (isError) return <ErrorState title={t('images.loadBuildsFailed')} message={failureReason(error, t)} onRetry={() => refetch()} isRetrying={isFetching} />;
    if (builds.length === 0) {
      return (
        <EmptyState
          title={t('images.emptyBuilds')}
          description={t('images.emptyBuildsDescription')}
          action={onBuild ? { label: t('images.buildFirst'), icon: Plus, onClick: onBuild } : undefined}
        />
      );
    }
    if (shown.length === 0) return <NoMatchesState query={view.search} onClear={() => changeView({ search: '' })} />;
    return (
      <List label={t('images.tab.builds')}>
        {shown.map((build) => <BuildRow key={build.id} build={build} onOpen={() => onOpen(build)} />)}
      </List>
    );
  };

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('images.title')}
          count={data?.total_count}
          pills={<>{pills}<ToolbarSearch label={t('images.searchBuildsLabel')} value={view.search} onChange={(search) => changeView({ search })} /></>}
          actions={<ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching} onClick={() => refetch()} />}
          primary={primary}
        />
      }
      head={(isLoading || shown.length > 0) && (
        <ListHead columns={columns} template={buildListLayout.wide} sort={view.sort} onSort={(key) => changeView({ sort: toggleSort(view.sort, key) })} />
      )}
      className="space-y-2"
    >
      {content()}
      {data && <Pagination currentPage={view.page} pageSize={pageSize} totalItems={data.total_count} onPageChange={(page) => changeView({ page })} />}
    </Page>
  );
}
