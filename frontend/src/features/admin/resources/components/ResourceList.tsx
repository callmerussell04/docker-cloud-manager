import type { ReactNode } from 'react';
import { ListFilter, RefreshCcw } from 'lucide-react';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { useListState } from '@/shared/lib/hooks';
import { toggleSort, type ListSort, type ListState, type ListStateSpec } from '@/shared/lib/listState';
import { EmptyState } from '@/shared/ui/EmptyState';
import { ErrorState } from '@/shared/ui/ErrorState';
import { List, ListHead, type ListColumn } from '@/shared/ui/List';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { NoMatchesState } from '@/shared/ui/NoMatchesState';
import { Pagination } from '@/shared/ui/Pagination';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { StateMessage } from '@/shared/ui/StateMessage';
import { ToolbarButton, ToolbarSearch } from '@/shared/ui/Toolbar';
import { filterByOwner, ownerName, ownerOptions, sortOwned, type Owned } from '../model';
import { FilterMenu, type FilterAlert, type FilterOption } from './FilterMenu';
import { OwnerFilter } from './OwnerFilter';

// Every all-users list reads the server's largest page; the owner filter
// applies to the page shown, and so do the sort and the search unless the
// server applies them (the containers list).
export const resourcePageSize = 100;

interface ListQuery<T> {
  data?: { items: T[]; total_count: number };
  isLoading: boolean;
  isError: boolean;
  isFetching: boolean;
  error: unknown;
}

// ResourceView is what every tab shares: the owner from the URL, with its
// change, and the accounts the owner filter offers.
export interface ResourceView {
  // owner is the chosen owner's user ID; empty for everyone.
  owner: string;
  onOwner: (owner: string) => void;
  accounts: readonly { user_id: string; username: string }[];
}

// StatusFilter is a status filter the server applies to a tab, shown as a
// capsule beside the owner filter: its choices (the first shows
// everything) with their counts, a count to call out while it shows
// everything, and what to say when it leaves nothing.
export interface StatusFilter<F extends string> {
  label: string;
  options: readonly FilterOption<F>[];
  alert?: FilterAlert;
  emptyTitle: string;
  emptyDescription: string;
  showAll: string;
}

export interface ResourceListProps<T extends Owned, K extends string, F extends string = 'all'> {
  view: ResourceView;
  // label names the list; columns are the resource's own, Owner goes after
  // the first; template is the rows' grid with the owner column.
  label: string;
  columns: readonly ListColumn<K>[];
  template: string;
  // loadingColumns shapes the placeholder rows on the first load.
  loadingColumns: number[];
  // spec is the list's URL state (resourceListSpec), the same the tab reads
  // when it asks the server for its sort, search or status.
  spec: ListStateSpec<F, K | 'owner'>;
  // statusFilter is the tab's status filter, applied by the server.
  statusFilter?: StatusFilter<F>;
  // count is the toolbar's count when no owner is chosen; the page's total
  // by default.
  count?: number;
  // sort and matches order and search the page shown. Without them the
  // server has sorted and searched it, by the same URL state: rows keep its
  // order (the Owner column still sorts the page), and changing the sort or
  // the search returns to the first page.
  sort?: (rows: readonly T[], sort: ListSort<K>) => T[];
  matches?: (row: T, search: string) => boolean;
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

// ResourceListParts is what a tab gives the all-users page: the toolbar's
// count and its actions (the filters, the search and refresh), the list's
// column labels, and its rows with their pages.
export interface ResourceListParts {
  count?: number;
  actions: ReactNode;
  head: ReactNode;
  body: ReactNode;
}

// useResourceList is one tab of the all-users view: a list of every user's
// resources of one kind with an Owner column, sorted, searched and narrowed
// to one owner (and, for a tab the server filters, to one status), all kept
// in the URL. It gives the parts the page shows, and nothing while the tab
// is hidden (active false).
export function useResourceList<T extends Owned, K extends string, F extends string = 'all'>(props: ResourceListProps<T, K, F>, active: boolean): ResourceListParts | null {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const { spec, statusFilter } = props;
  const [view, setView] = useListState(spec);
  const serverSide = !props.matches;
  // The server pages what the status, sort and search leave, so changing
  // them returns to the first page.
  const changeView = (change: Partial<ListState<F, K | 'owner'>>) =>
    setView(serverSide && change.page === undefined && (change.filter !== undefined || change.sort || change.search !== undefined) ? { ...change, page: 1 } : change);
  const everything = spec.filters[0];
  const { data, isLoading, isError, isFetching, error } = props.query;
  // On narrow content the filters become round buttons, so the top row keeps
  // room for the title.
  const narrow = useShellLayout().toolbarStacked;
  if (!active) return null;
  const rows = data?.items ?? [];
  const { owner } = props.view;
  const owners = ownerOptions(rows, props.view.accounts);
  const owned = filterByOwner(rows, owner);
  const { matches, sort } = props;
  const searched = matches ? owned.filter((row) => matches(row, view.search)) : owned;
  const shown = sortOwned(searched, view.sort, sort ?? ((rows) => [...rows]), props.nameOf);
  const ownerLabel = (row: T) => ownerName(row) || t('admin.resources.ownerUnknown');

  const [first, ...rest] = props.columns;
  const columns: ListColumn<K | 'owner'>[] = [first, { key: 'owner', label: t('admin.resources.owner') }, ...rest];

  const content = () => {
    if (isLoading) return <LoadingRows columns={compact ? [1] : props.loadingColumns} />;
    if (isError) return <ErrorState title={props.loadFailed} message={failureReason(error, t)} onRetry={props.onRefresh} isRetrying={isFetching} />;
    if (rows.length === 0 && serverSide) {
      const searched = view.search.trim() !== '';
      const nothingMatches = statusFilter?.options.find((option) => option.id === everything)?.count === 0;
      if (searched && (!statusFilter || nothingMatches)) return <NoMatchesState query={view.search} onClear={() => changeView({ search: '' })} />;
      if (statusFilter && view.filter !== everything) {
        return (
          <StateMessage
            role="status"
            title={statusFilter.emptyTitle}
            description={statusFilter.emptyDescription}
            action={{ label: statusFilter.showAll, onClick: () => changeView({ filter: everything }) }}
          />
        );
      }
      if (searched) return <NoMatchesState query={view.search} onClear={() => changeView({ search: '' })} />;
    }
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

  const ownerFilter = <OwnerFilter owner={owner} options={owners} onChange={props.view.onOwner} round={narrow} />;
  const chosenStatus = statusFilter?.options.find((option) => option.id === view.filter);
  const statusCapsule = statusFilter && (
    <FilterMenu
      label={statusFilter.label}
      icon={ListFilter}
      value={view.filter}
      chosen={chosenStatus?.label ?? view.filter}
      count={chosenStatus?.count}
      options={statusFilter.options}
      onChange={(filter) => changeView({ filter })}
      narrowed={view.filter !== everything}
      // Called out only while everything shows; a chosen status says its own count.
      alert={view.filter === everything ? statusFilter.alert : undefined}
      round={narrow}
    />
  );
  const search = <ToolbarSearch label={props.searchLabel} value={view.search} onChange={(search) => changeView({ search })} />;

  return {
    // With an owner chosen, the count is theirs on the page shown.
    count: owner && data ? owned.length : (props.count ?? data?.total_count),
    // The filters and the search sit with the actions, so they stay in the
    // top row when the tabs move to a second one and never crowd it.
    actions: (
      <>
        {statusCapsule}
        {ownerFilter}
        {search}
        <ToolbarButton icon={RefreshCcw} label={t('common.refresh')} busy={isFetching || props.refreshing} onClick={props.onRefresh} />
      </>
    ),
    head: (isLoading || shown.length > 0) && (
      <ListHead columns={columns} template={props.template} sort={view.sort} onSort={(key) => changeView({ sort: toggleSort(view.sort, key) })} />
    ),
    body: (
      <>
        {content()}
        {data && <Pagination currentPage={view.page} pageSize={resourcePageSize} totalItems={data.total_count} onPageChange={(page) => changeView({ page })} />}
      </>
    ),
  };
}
