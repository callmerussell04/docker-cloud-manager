import { describe, expect, it } from 'vitest';
import { isPaletteShortcut, moveActive, visibleGroups, type PaletteGroup } from './searchPalette';

const item = (id: string, label: string, keywords?: string) => ({ id, label, keywords });

const groups: PaletteGroup<ReturnType<typeof item>>[] = [
  { id: 'actions', label: 'Actions', items: [item('new', 'New container'), item('theme', 'Switch appearance')] },
  { id: 'pages', label: 'Go to', items: [item('/', 'Dashboard'), item('/containers', 'Containers')] },
  {
    id: 'containers',
    label: 'Containers',
    whenSearching: true,
    limit: 2,
    items: [item('c1', 'shop', 'nginx:1.27'), item('c2', 'shop-db', 'postgres:17'), item('c3', 'blog', 'ghost:5'), item('c4', 'shop-cache', 'redis:7')],
  },
];

const ids = (result: PaletteGroup<ReturnType<typeof item>>[]) => result.map((group) => [group.id, group.items.map((entry) => entry.id)]);

describe('visibleGroups', () => {
  it('keeps the items whose label matches the query, ignoring case and surrounding space', () => {
    expect(ids(visibleGroups(groups, '  CONTAIN '))).toEqual([
      ['actions', ['new']],
      ['pages', ['/containers']],
    ]);
  });

  it('matches keywords as well as the label', () => {
    expect(ids(visibleGroups(groups, 'postgres'))).toEqual([['containers', ['c2']]]);
  });

  it('shows a group kept for searching only once there is a query, and no more of it than its limit', () => {
    expect(ids(visibleGroups(groups, ''))).toEqual([
      ['actions', ['new', 'theme']],
      ['pages', ['/', '/containers']],
    ]);
    expect(ids(visibleGroups(groups, 'shop'))).toEqual([['containers', ['c1', 'c2']]]);
  });

  it('shows nothing when nothing matches', () => {
    expect(visibleGroups(groups, 'zzz')).toEqual([]);
  });
});

describe('moveActive', () => {
  it.each([
    [0, 1, 3, 1],
    [2, 1, 3, 0],
    [0, -1, 3, 2],
    [1, -1, 3, 0],
    [0, 1, 0, 0],
    [0, -1, 0, 0],
  ])('moves from row %i by %i among %i rows to row %i, wrapping at the ends', (from, by, count, expected) => {
    expect(moveActive(from, by, count)).toBe(expected);
  });
});

describe('isPaletteShortcut', () => {
  const key = (k: string, mods: Partial<Record<'ctrlKey' | 'metaKey' | 'altKey' | 'shiftKey', boolean>> = {}) => ({
    key: k,
    ctrlKey: false,
    metaKey: false,
    altKey: false,
    shiftKey: false,
    ...mods,
  });

  it.each([
    ['Ctrl K elsewhere', false, true, key('k', { ctrlKey: true })],
    ['Ctrl K with Caps Lock elsewhere', false, true, key('K', { ctrlKey: true })],
    ['Cmd K on a Mac', true, true, key('k', { metaKey: true })],
    ['Ctrl K on a Mac, where it deletes to the end of the line', true, false, key('k', { ctrlKey: true })],
    ['the Windows key and K elsewhere', false, false, key('k', { metaKey: true })],
    ['K alone', false, false, key('k')],
    ['Ctrl Shift K', false, false, key('K', { ctrlKey: true, shiftKey: true })],
    ['Ctrl Alt K', false, false, key('k', { ctrlKey: true, altKey: true })],
    ['Ctrl J', false, false, key('j', { ctrlKey: true })],
  ])('%s (Mac: %s) opens the palette: %s', (_name, mac, expected, event) => {
    expect(isPaletteShortcut(event, mac)).toBe(expected);
  });
});
