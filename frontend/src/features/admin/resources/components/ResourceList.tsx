import { useMemo, type ReactNode } from 'react';
import { RefreshCcw } from 'lucide-react';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { useListState } from '@/shared/lib/hooks';
import { toggleSort, type ListSort } from '@/shared/lib/listState';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorState } from '@/shared/ui/ErrorState';
import { List, ListHead, type ListColumn } from '@/shared/ui/List';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { Page } from '@/shared/ui/Page';
import { Pagination } from '@/shared/ui/Pagination';
import { Pills } from '@/shared/ui/Pills';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { StateMessage } from '@/shared/ui/StateMessage';
import { Toolbar, ToolbarButton, ToolbarSearch } from '@/shared/ui/Toolbar';
import { filterByOwner, ownerName, ownerOptions, resourceTabs, sortOwned, type Owned, type ResourceTab } from '../model';
import { OwnerFilter } from './OwnerFilter';

// Every all-users list reads the server's largest page; the owner filter,
// the sort and the search apply to the page shown.
export const resourcePageSize = 100;

interface ListQuery<T> {
  data?: { items: T[]; total_count: number };
  isLoading: boolean;
  isError: boolean;
  isFetching: boolean;
  error: unknown;
}

// ResourceView is what every tab shares: the tab and the owner from the URL,
// with their changes, and the accounts the owner filter offers.
export interface ResourceView {
  tab: ResourceTab;
  onTab: (tab: ResourceTab) => void;
  // owner is the chosen owner's user ID; empty for everyone.
  owner: string;
  onOwner: (owner: string) => void;
  accounts: readonly { user_id: string; username: string }[];
}

export interface ResourceListProps<T extends Owned, K extends string> {
  view: ResourceView;
  // label names the list; columns are the resource's own, Owner goes after
  // the first; template is the rows' grid with the owner column.
  label: string;
  columns: readonly ListColumn<K>[];
  template: string;
  // loadingColumns shapes the placeholder rows on the first load.
  loadingColumns: number[];
  sortKeys: readonly K[];
  defaultSort: ListSort<K>;
  sort: (rows: readonly T[], sort: ListSort<K>) => T[];
  matches: (row: T, search: string) => boolean;
  nameOf: (row: T) => string;
  searchLabel: string;
  loadFailed: string;
  emptyTitle: string;
  // query is the page the URL asks for (readListPage), read by the tab.
  query: ListQuery<T>;
  onRefresh: () => void;
  refreshing?: boolean;
  row: (row: T, owner: string) => ReactNode;
}

// ResourceList is one tab of the all-users view: a list of every user's
// resources of one kind with an Owner column, sorted, searched and narrowed
// to one owner, all kept in the URL.
export function ResourceList<T extends Owned, K extends string>(props: ResourceListProps<T, K>) {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const { sortKeys, defaultSort } = props;
  const spec = useMemo(() => ({
    filters: ['all'] as const,
    sortKeys: [...sortKeys, 'owner'] as readonly (K | 'owner')[],
    defaultSort: defaultSort as ListSort<K | 'owner'>,
  }), [sortKeys, defaultSort]);
  const [view, changeView] = useListState(spec);
  const { data, isLoading, isError, isFetching, error } = props.query;
  // A narrow toolbar stacks its pills on a second row that the five tabs
  // already fill, so the owner filter (as a round button) and the search
  // join the refresh button in the top row instead.
  const narrow = useShellLayout().toolbarStacked;
  const rows = data?.items ?? [];
  const { owner } = props.view;
  const owners = ownerOptions(rows, props.view.accounts);
  const owned = filterByOwner(rows, owner);
  const searched = owned.filter((row) => props.matches(row, view.search));
  const shown = sortOwned(searched, view.sort, props.sort, props.nameOf);
  const ownerLabel = (row: T) => ownerName(row) || t('admin.resources.ownerUnknown');

  const [first, ...rest] = props.columns;
  const columns: ListColumn<K | 'owner'>[] = [first, { key: 'owner', label: t('admin.resources.owner') }, ...rest];

  const content = () => {
    if (isLoading) return <LoadingRows columns={compact ? [1] : props.loadingColumns} />;
    if (isError) return <ErrorState title={props.loadFailed} message={failureReason(error, t)} onRetry={props.onRefresh} isRetrying={isFetching} />;
    if (rows.length === 0) return <EmptyState title={props.emptyTitle} description={t('admin.resources.emptyDescription')} />;
    if (owned.length === 0) {
      const name = owners.find((option) => option.id === owner)?.name ?? owner;
      return (
        <StateMessage
          role="status"
          title={t('admin.resources.ownerEmpty', { name })}
          description={t('admin.resources.ownerEmptyDescription')}
          action={{ label: t('admin.resources.showEveryone'), onClick: () => props.view.onOwner('') }}
        />
      );
    }
    if (shown.length === 0) return <NoMatchesState query={view.search} onClear={() => changeView({ search: '' })} />;
    return <List label={props.label}>{shown.map((row) => props.row(row, ownerLabel(row)))}</List>;
  };

  const tabs = (
    <Pills
      label={t('admin.resources.tabsLabel')}
      value={props.view.tab}
      onChange={props.view.onTab}
      dense={narrow}
      items={resourceTabs.map((id) => ({ id, label: t(`admin.resources.tab.${id}`) }))}
    />
  );
  const ownerFilter = <OwnerFilter owner={owner} options={owners} onChange={props.view.onOwner} round={narrow} />;
  const search = <ToolbarSearch label={props.searchLabel} value={view.search} onChange={(search) => changeView({ search })} />;

  return (
    <Page
      toolbar={
        <Toolbar
          title={t('admin.resources.title')}
          // With an owner chosen, the count is theirs on the page shown.
          count={owner && data ? owned.length : data?.total_count}
          pills={
            <div className="flex items-center gap-2">
              {tabs}
              {!narrow && ownerFilter}
              {!narrow && search}
            </div>
          }
          actions={
            <>
              {narrow && ownerFilter}
              {narrow && search}
              <ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching || props.refreshing} onClick={props.onRefresh} />
            </>
          }
        />
      }
      head={(isLoading || shown.length > 0) && (
        <ListHead columns={columns} template={props.template} sort={view.sort} onSort={(key) => changeView({ sort: toggleSort(view.sort, key) })} />
      )}
      className="space-y-2"
    >
      {content()}
      {data && <Pagination currentPage={view.page} pageSize={resourcePageSize} totalItems={data.total_count} onPageChange={(page) => changeView({ page })} />}
    </Page>
  );
}
