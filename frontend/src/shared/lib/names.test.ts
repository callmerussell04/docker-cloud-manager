import { describe, expect, test } from 'vitest';
import { joinNames, matchesSearch, shortNames } from './names';

const more = (names: string, count: number) => `${names} +${count}`;

describe('joinNames', () => {
  test('lists every name in order', () => {
    expect(joinNames([])).toBe('');
    expect(joinNames([{ name: 'web' }, { name: 'api' }, { name: 'jobs' }])).toBe('web, api, jobs');
  });
});

describe('shortNames', () => {
  test('is nothing when there are no items', () => {
    expect(shortNames([], more)).toBeUndefined();
  });

  test('names up to two items', () => {
    expect(shortNames([{ name: 'web' }], more)).toBe('web');
    expect(shortNames([{ name: 'web' }, { name: 'api' }], more)).toBe('web, api');
  });

  test('words how many more there are beyond two', () => {
    expect(shortNames([{ name: 'web' }, { name: 'api' }, { name: 'jobs' }, { name: 'cron' }], more)).toBe('web, api +2');
  });
});

describe('matchesSearch', () => {
  test.each([
    ['', true],
    ['   ', true],
    ['shop', true],
    ['  SHOP ', true],
    ['WORKER', true],
    ['redis', false],
  ])('matchesSearch(%j) -> %s', (search, expected) => {
    expect(matchesSearch(search, ['shop:v1', undefined, 'worker'])).toBe(expected);
  });

  test('a missing text matches nothing', () => {
    expect(matchesSearch('x', [undefined])).toBe(false);
  });
});
