import { sortRows, type ListSort, type ListStateSpec } from '@/shared/lib/listState';

// The all-users view's tabs, switched by the toolbar's pills and kept in the
// URL as ?tab= (containers is the default and left out), and its owner filter,
// ?owner=<user id> (everyone is the default and left out). See
// docs/agent-context/product-frontend.md, "Users and all resources".
export const resourceTabs = ['containers', 'volumes', 'images', 'builds', 'projects'] as const;
export type ResourceTab = (typeof resourceTabs)[number];

const tabParam = 'tab';
const ownerParam = 'owner';

export function readResourceTab(params: URLSearchParams): ResourceTab {
  return resourceTabs.find((tab) => tab === params.get(tabParam)) ?? 'containers';
}

// readOwnerFilter is the owner whose resources are shown, by user ID; empty
// for everyone.
export function readOwnerFilter(params: URLSearchParams) {
  return params.get(ownerParam) ?? '';
}

// withResourceTab switches tabs: the owner stays, and the other list starts
// fresh, without the last one's sort, page or search.
export function withResourceTab(params: URLSearchParams, tab: ResourceTab) {
  const next = new URLSearchParams();
  if (tab !== 'containers') next.set(tabParam, tab);
  const owner = readOwnerFilter(params);
  if (owner) next.set(ownerParam, owner);
  return next;
}

// withOwnerFilter chooses an owner, or everyone for an empty one, keeping the
// rest of the view.
export function withOwnerFilter(params: URLSearchParams, owner: string) {
  const next = new URLSearchParams(params);
  if (owner) next.set(ownerParam, owner);
  else next.delete(ownerParam);
  return next;
}

export interface Owned {
  owner_id?: string;
  owner_username?: string;
}

// ownerName names a resource's owner: the username, or the ID the API gave
// without one; empty when it gave neither.
export function ownerName(resource: Owned) {
  return resource.owner_username || resource.owner_id || '';
}

export interface OwnerOption {
  id: string;
  name: string;
}

// ownerOptions is who the owner filter offers: every account given and every
// owner on the page read (an account past the users read still shows), once
// each, by name.
export function ownerOptions(rows: readonly Owned[], users: readonly { user_id: string; username: string }[]): OwnerOption[] {
  const options = new Map<string, string>();
  for (const user of users) options.set(user.user_id, user.username);
  for (const resource of rows) {
    if (resource.owner_id && !options.has(resource.owner_id)) options.set(resource.owner_id, ownerName(resource));
  }
  return sortRows([...options].map(([id, name]) => ({ id, name })), (option) => [option.name, option.id], true);
}

// filterByOwner keeps one owner's rows; an empty owner keeps every row.
export function filterByOwner<T extends Owned>(rows: readonly T[], owner: string): T[] {
  return owner ? rows.filter((resource) => resource.owner_id === owner) : [...rows];
}

// sortOwned orders an all-users list: by owner (names breaking ties, which
// never reverse) for the Owner column, and by the resource's own sort for
// every other column.
export function sortOwned<T extends Owned, K extends string>(
  rows: readonly T[],
  sort: ListSort<K | 'owner'>,
  sortOwn: (rows: readonly T[], sort: ListSort<K>) => T[],
  nameOf: (row: T) => string,
): T[] {
  if (sort.key === 'owner') return sortRows(rows, (resource) => [ownerName(resource), nameOf(resource)], sort.ascending);
  return sortOwn(rows, sort as ListSort<K>);
}

// resourceListSpec is an all-users list's URL state: its own columns and
// Owner, with no filter. A tab that asks the server for its sort and search
// reads the same state with it.
export function resourceListSpec<K extends string>(sortKeys: readonly K[], defaultSort: ListSort<K>): ListStateSpec<'all', K | 'owner'> {
  return {
    filters: ['all'] as const,
    sortKeys: [...sortKeys, 'owner'],
    defaultSort: defaultSort as ListSort<K | 'owner'>,
  };
}
