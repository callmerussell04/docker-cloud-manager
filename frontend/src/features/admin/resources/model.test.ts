import { describe, expect, test } from 'vitest';
import type { ListSort } from '@/shared/lib/listState';
import {
  filterByOwner,
  ownerName,
  ownerOptions,
  readOwnerFilter,
  readResourceTab,
  sortOwned,
  withOwnerFilter,
  withResourceTab,
} from './model';

interface Row {
  id: string;
  name: string;
  owner_id?: string;
  owner_username?: string;
}

const row = (name: string, owner_id: string, owner_username?: string): Row => ({ id: name, name, owner_id, owner_username });

describe('the tab and owner in the URL', () => {
  test('reads containers, and everyone, when the URL names neither', () => {
    const params = new URLSearchParams();
    expect(readResourceTab(params)).toBe('containers');
    expect(readOwnerFilter(params)).toBe('');
  });

  test('reads a known tab and the owner', () => {
    const params = new URLSearchParams('tab=builds&owner=u1');
    expect(readResourceTab(params)).toBe('builds');
    expect(readOwnerFilter(params)).toBe('u1');
  });

  test('reads an unknown tab as containers', () => {
    expect(readResourceTab(new URLSearchParams('tab=secrets'))).toBe('containers');
  });

  test('switching tabs keeps the owner and starts the list fresh', () => {
    const next = withResourceTab(new URLSearchParams('tab=volumes&owner=u1&sort=-size&page=2&q=pg'), 'images');
    expect(next.toString()).toBe('tab=images&owner=u1');
  });

  test('switching to containers leaves the default tab out', () => {
    expect(withResourceTab(new URLSearchParams('tab=volumes'), 'containers').toString()).toBe('');
  });

  test('choosing an owner keeps the rest of the view, and everyone drops it', () => {
    const params = new URLSearchParams('tab=volumes&sort=-size');
    expect(withOwnerFilter(params, 'u2').toString()).toBe('tab=volumes&sort=-size&owner=u2');
    expect(withOwnerFilter(new URLSearchParams('owner=u2&q=pg'), '').toString()).toBe('q=pg');
  });
});

test.each([
  ['the username', row('a', 'u1', 'alice'), 'alice'],
  ['the ID without a username', row('a', 'u1'), 'u1'],
  ['nothing without either', { id: 'a', name: 'a' }, ''],
])('ownerName gives %s', (_, resource, expected) => {
  expect(ownerName(resource)).toBe(expected);
});

describe('ownerOptions', () => {
  test('offers every account and every owner on the page once, by name', () => {
    const users = [{ user_id: 'u2', username: 'bob' }, { user_id: 'u1', username: 'alice' }];
    const rows = [row('a', 'u1', 'alice'), row('b', 'u3', 'carol'), row('c', 'u3', 'carol'), row('d', 'u4')];
    expect(ownerOptions(rows, users)).toEqual([
      { id: 'u1', name: 'alice' },
      { id: 'u2', name: 'bob' },
      { id: 'u3', name: 'carol' },
      { id: 'u4', name: 'u4' },
    ]);
  });

  test('leaves out a row with no owner', () => {
    expect(ownerOptions([{ id: 'a', name: 'a' } as Row], [])).toEqual([]);
  });
});

test('filterByOwner keeps one owner\'s rows, or every row for everyone', () => {
  const rows = [row('a', 'u1'), row('b', 'u2'), row('c', 'u1')];
  expect(filterByOwner(rows, 'u1').map((entry) => entry.name)).toEqual(['a', 'c']);
  expect(filterByOwner(rows, '')).toEqual(rows);
});

describe('sortOwned', () => {
  const rows = [row('web', 'u2', 'bob'), row('api', 'u1', 'alice'), row('db', 'u2', 'bob')];
  const byNameDescending = (list: readonly Row[]) => [...list].sort((a, b) => b.name.localeCompare(a.name));
  const names = (list: Row[]) => list.map((entry) => entry.name);

  test('sorts by owner, names breaking the tie whichever way it goes', () => {
    expect(names(sortOwned(rows, { key: 'owner', ascending: true }, byNameDescending, (entry) => entry.name))).toEqual(['api', 'db', 'web']);
    expect(names(sortOwned(rows, { key: 'owner', ascending: false }, byNameDescending, (entry) => entry.name))).toEqual(['db', 'web', 'api']);
  });

  test('leaves every other column to the resource\'s own sort', () => {
    const sort: ListSort<'name' | 'owner'> = { key: 'name', ascending: true };
    expect(names(sortOwned(rows, sort, byNameDescending, (entry) => entry.name))).toEqual(['web', 'db', 'api']);
  });
});
