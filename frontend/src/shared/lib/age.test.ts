import { describe, expect, test } from 'vitest';
import { ageOf } from './age';

describe('ageOf', () => {
  const now = 1_800_000_000;
  const minute = 60;
  const hour = 60 * minute;
  const day = 24 * hour;

  test.each([
    [0, 1, 'minute'],
    [30, 1, 'minute'],
    [42 * minute, 42, 'minute'],
    [59 * minute, 59, 'minute'],
    [hour, 1, 'hour'],
    [5 * hour + 40 * minute, 5, 'hour'],
    [23 * hour, 23, 'hour'],
    [day, 1, 'day'],
    [12 * day, 12, 'day'],
    [59 * day, 59, 'day'],
    [60 * day, 2, 'month'],
    [364 * day, 12, 'month'],
    [365 * day, 1, 'year'],
    [800 * day, 2, 'year'],
  ])('%i seconds old is %i %s', (seconds, count, unit) => {
    expect(ageOf(now - seconds, now)).toEqual({ count, unit });
  });

  test('a time in the future, as from a skewed clock, is a minute', () => {
    expect(ageOf(now + hour, now)).toEqual({ count: 1, unit: 'minute' });
  });
});
