// The view state every list keeps: which column sorts it and which way, and
// the rules to order its rows. See docs/agent-context/product-frontend.md,
// "Lists".

export interface ListSort<K extends string> {
  key: K;
  ascending: boolean;
}

// toggleSort is the sort after a column label is clicked: a new column sorts
// ascending, the current one reverses.
export function toggleSort<K extends string>(current: ListSort<K>, key: K): ListSort<K> {
  return { key, ascending: current.key === key ? !current.ascending : true };
}

type SortValue = string | number;

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

function compareValue(a: SortValue, b: SortValue) {
  if (typeof a === 'number' && typeof b === 'number') return a - b;
  return collator.compare(String(a), String(b));
}

// sortRows returns the rows ordered by the value each one gives, text
// naturally and ignoring case ("web-2" before "web-10"). A value given as a
// list breaks ties with its next entries, which never reverse with the order,
// so equal rows keep one steady order either way.
export function sortRows<T>(rows: readonly T[], value: (row: T) => SortValue | readonly SortValue[], ascending: boolean): T[] {
  return [...rows].sort((a, b) => {
    const left = [value(a)].flat();
    const right = [value(b)].flat();
    const first = compareValue(left[0], right[0]);
    if (first !== 0) return ascending ? first : -first;
    for (let index = 1; index < Math.max(left.length, right.length); index++) {
      const next = compareValue(left[index] ?? '', right[index] ?? '');
      if (next !== 0) return next;
    }
    return 0;
  });
}

// ListState is what a list keeps in its URL, so refreshing the page or
// sharing its link shows the same rows: the filter, the sort, the page and
// the search.
export interface ListState<F extends string, K extends string> {
  filter: F;
  sort: ListSort<K>;
  page: number;
  search: string;
}

// serverViewChange is a change to the view of a list the server pages,
// returning to the first page when the filter, sort or search changes: the
// server pages the rows they shape, so the old page may no longer exist.
export function serverViewChange<C extends Partial<ListState<string, string>>>(change: C): C {
  if (change.page !== undefined) return change;
  if (change.filter === undefined && change.sort === undefined && change.search === undefined) return change;
  return { ...change, page: 1 };
}

// ListStateSpec names a list's filters (the first is the default) and its
// sortable columns.
export interface ListStateSpec<F extends string, K extends string> {
  filters: readonly F[];
  sortKeys: readonly K[];
  defaultSort: ListSort<K>;
}

// The search parameters: ?filter=running&sort=-memory&page=2&q=web. A
// descending sort is its column with a leading minus.
const param = { filter: 'filter', sort: 'sort', page: 'page', search: 'q' } as const;

function readSort<K extends string>(value: string | null, spec: ListStateSpec<string, K>): ListSort<K> {
  if (!value) return spec.defaultSort;
  const ascending = !value.startsWith('-');
  const key = spec.sortKeys.find((candidate) => candidate === (ascending ? value : value.slice(1)));
  return key ? { key, ascending } : spec.defaultSort;
}

// readListPage is the list page a URL asks for; anything but a whole number
// from 1 is the first page.
export function readListPage(params: URLSearchParams) {
  return readPage(params.get(param.page));
}

function readPage(value: string | null) {
  const page = Number(value);
  return Number.isInteger(page) && page >= 1 ? page : 1;
}

// readListState reads a list's state from the URL; a value it does not know
// is the default.
export function readListState<F extends string, K extends string>(params: URLSearchParams, spec: ListStateSpec<F, K>): ListState<F, K> {
  const filter = spec.filters.find((candidate) => candidate === params.get(param.filter)) ?? spec.filters[0];
  return {
    filter,
    sort: readSort(params.get(param.sort), spec),
    page: readPage(params.get(param.page)),
    search: params.get(param.search) ?? '',
  };
}

// writeListState puts a list's state into the URL's parameters, leaving out
// each default and keeping every other parameter.
export function writeListState<F extends string, K extends string>(params: URLSearchParams, state: ListState<F, K>, spec: ListStateSpec<F, K>): URLSearchParams {
  const next = new URLSearchParams(params);
  const set = (name: string, value: string | null) => (value === null ? next.delete(name) : next.set(name, value));
  const defaultSort = state.sort.key === spec.defaultSort.key && state.sort.ascending === spec.defaultSort.ascending;
  set(param.filter, state.filter === spec.filters[0] ? null : state.filter);
  set(param.sort, defaultSort ? null : `${state.sort.ascending ? '' : '-'}${state.sort.key}`);
  set(param.page, state.page > 1 ? String(state.page) : null);
  set(param.search, state.search ? state.search : null);
  return next;
}

