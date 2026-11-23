import { describe, expect, test } from 'vitest';
import { readListState, sortRows, toggleSort, writeListState, type ListStateSpec } from './listState';

describe('toggleSort', () => {
  test('a new column sorts ascending', () => {
    expect(toggleSort({ key: 'name', ascending: false }, 'memory')).toEqual({ key: 'memory', ascending: true });
  });

  test('the same column again reverses the order', () => {
    expect(toggleSort({ key: 'name', ascending: true }, 'name')).toEqual({ key: 'name', ascending: false });
    expect(toggleSort({ key: 'name', ascending: false }, 'name')).toEqual({ key: 'name', ascending: true });
  });
});

describe('sortRows', () => {
  const rows = [
    { name: 'web-10', size: 3 },
    { name: 'api', size: 1 },
    { name: 'Web-2', size: 2 },
  ];

  test('orders text naturally and ignoring case', () => {
    expect(sortRows(rows, (row) => row.name, true).map((row) => row.name)).toEqual(['api', 'Web-2', 'web-10']);
  });

  test('orders numbers by value, either way', () => {
    expect(sortRows(rows, (row) => row.size, true).map((row) => row.size)).toEqual([1, 2, 3]);
    expect(sortRows(rows, (row) => row.size, false).map((row) => row.size)).toEqual([3, 2, 1]);
  });

  test('breaks ties with the next value given', () => {
    const tied = [
      { group: 1, name: 'b' },
      { group: 0, name: 'z' },
      { group: 1, name: 'a' },
    ];
    expect(sortRows(tied, (row) => [row.group, row.name], true).map((row) => row.name)).toEqual(['z', 'a', 'b']);
  });

  test('leaves the input unchanged', () => {
    const input = [...rows];
    sortRows(input, (row) => row.size, true);
    expect(input).toEqual(rows);
  });
});

describe('list state in the URL', () => {
  const spec: ListStateSpec<'all' | 'running', 'name' | 'status'> = {
    filters: ['all', 'running'],
    sortKeys: ['name', 'status'],
    defaultSort: { key: 'status', ascending: true },
  };
  const read = (query: string) => readListState(new URLSearchParams(query), spec);
  const write = (query: string, state: ReturnType<typeof read>) => writeListState(new URLSearchParams(query), state, spec).toString();

  test('an empty query is the first filter, the default sort, page 1 and no search', () => {
    expect(read('')).toEqual({ filter: 'all', sort: { key: 'status', ascending: true }, page: 1, search: '' });
  });

  test('reads filter, sort, page and search', () => {
    expect(read('filter=running&sort=-name&page=3&q=web')).toEqual({ filter: 'running', sort: { key: 'name', ascending: false }, page: 3, search: 'web' });
    expect(read('sort=name').sort).toEqual({ key: 'name', ascending: true });
  });

  test('falls back to the defaults for values it does not know', () => {
    expect(read('filter=gone&sort=-size&page=0')).toEqual(read(''));
    expect(read('page=two').page).toBe(1);
    expect(read('page=2.5').page).toBe(1);
  });

  test('writes only what differs from the defaults, keeping other parameters', () => {
    expect(write('new', read(''))).toBe('new=');
    expect(write('', { filter: 'running', sort: { key: 'name', ascending: false }, page: 2, search: 'web api' })).toBe('filter=running&sort=-name&page=2&q=web+api');
  });

  test('writing what it read gives the same state back', () => {
    const state = read('filter=running&sort=name&page=4&q=db');
    expect(readListState(writeListState(new URLSearchParams(), state, spec), spec)).toEqual(state);
  });
});

