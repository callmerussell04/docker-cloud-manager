import { describe, expect, test } from 'vitest';
import { meterParts } from './meter';

describe('meterParts', () => {
  test('parts are shares of the whole and the rest is free', () => {
    expect(meterParts([{ value: 25 }, { value: 50 }], 200)).toEqual({ parts: [{ value: 25, percent: 12.5 }, { value: 50, percent: 25 }], freePercent: 62.5 });
  });

  test('parts beyond the whole are cut at it, in order, leaving nothing free', () => {
    expect(meterParts([{ value: 60 }, { value: 60 }], 100)).toEqual({ parts: [{ value: 60, percent: 60 }, { value: 60, percent: 40 }], freePercent: 0 });
  });

  test('a negative part takes no room', () => {
    expect(meterParts([{ value: -5 }], 10)).toEqual({ parts: [{ value: -5, percent: 0 }], freePercent: 100 });
  });

  test('an empty whole has no free room', () => {
    expect(meterParts([{ value: 3 }], 0)).toEqual({ parts: [{ value: 3, percent: 0 }], freePercent: 0 });
  });
});
