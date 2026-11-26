// Named is anything a list shows by its name.
interface Named {
  name: string;
}

// joinNames lists every item by name, separated by commas.
export function joinNames(items: readonly Named[]) {
  return items.map((item) => item.name).join(', ');
}

// shortNames names up to two items, and words how many others there are
// with more; nothing when there are none.
export function shortNames(items: readonly Named[], more: (names: string, count: number) => string): string | undefined {
  if (items.length === 0) return undefined;
  const names = joinNames(items.slice(0, 2));
  return items.length > 2 ? more(names, items.length - 2) : names;
}

// matchesSearch reports whether any of the texts contains the search,
// ignoring case and the spaces around it; an empty search matches anything.
// A missing text matches nothing.
export function matchesSearch(search: string, texts: readonly (string | undefined)[]) {
  const query = search.trim().toLowerCase();
  return !query || texts.some((text) => text?.toLowerCase().includes(query));
}
