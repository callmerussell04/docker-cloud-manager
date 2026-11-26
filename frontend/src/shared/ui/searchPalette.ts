// The rules of the search palette (SearchPalette.tsx), kept apart from its
// rendering: which items a query shows, and how the active row moves.

export interface PaletteEntry {
  id: string;
  label: string;
  // keywords match as the label does without being shown as it, such as a
  // container's image.
  keywords?: string;
}

export interface PaletteGroup<I extends PaletteEntry> {
  id: string;
  label: string;
  items: I[];
  // whenSearching keeps the group hidden until something is typed, as the
  // user's containers are, which would otherwise crowd out the rest.
  whenSearching?: boolean;
  // limit is the most matches the group shows.
  limit?: number;
}

function matches(entry: PaletteEntry, query: string) {
  return `${entry.label} ${entry.keywords ?? ''}`.toLowerCase().includes(query);
}

// visibleGroups is what the palette shows for a query: the matching items of
// each group, in order, without the groups that show nothing.
export function visibleGroups<I extends PaletteEntry>(groups: PaletteGroup<I>[], query: string): PaletteGroup<I>[] {
  const needle = query.trim().toLowerCase();
  return groups
    .filter((group) => !group.whenSearching || needle !== '')
    .map((group) => ({ ...group, items: group.items.filter((entry) => matches(entry, needle)).slice(0, group.limit) }))
    .filter((group) => group.items.length > 0);
}

// moveActive moves the active row by a step among count rows, wrapping from
// the last row to the first and back; with no rows it stays at the first.
export function moveActive(index: number, step: number, count: number): number {
  if (count === 0) return 0;
  return (((index + step) % count) + count) % count;
}

interface ShortcutKey {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  altKey: boolean;
  shiftKey: boolean;
}

// isPaletteShortcut says whether a key press opens the palette: Cmd K on a
// Mac, where Ctrl K deletes to the end of the line, and Ctrl K elsewhere,
// with no other modifier.
export function isPaletteShortcut(event: ShortcutKey, mac: boolean): boolean {
  const command = mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
  return event.key.toLowerCase() === 'k' && command && !event.altKey && !event.shiftKey;
}

// onMac says whether the browser runs on Apple's systems, whose command key
// is Cmd.
export function onMac(): boolean {
  return /Mac|iPhone|iPad/.test(navigator.userAgent);
}

// paletteShortcut is the shortcut as the platform writes it, for hints beside
// the controls that open the palette and for aria-keyshortcuts.
export function paletteShortcut(): { label: string; aria: string } {
  return onMac() ? { label: '⌘K', aria: 'Meta+K' } : { label: 'Ctrl K', aria: 'Control+K' };
}
