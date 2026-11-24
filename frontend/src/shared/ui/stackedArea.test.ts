import { describe, expect, test } from 'vitest';
import { guideShares, nearestPoint, niceCeiling, niceScale, spreadLabels, stackBands } from './stackedArea';

describe('stackBands', () => {
  test('lays each series on top of those before it', () => {
    expect(stackBands([[1, 2], [3, 0], [5, 5]])).toEqual([
      { lower: [0, 0], upper: [1, 2] },
      { lower: [1, 2], upper: [4, 2] },
      { lower: [4, 2], upper: [9, 7] },
    ]);
  });
});

describe('niceCeiling', () => {
  test('rounds the top of the scale up to 1, 2, 2.5 or 5 times a power of ten', () => {
    expect(niceCeiling(7.3)).toBe(10);
    expect(niceCeiling(1.6)).toBe(2);
    expect(niceCeiling(230)).toBe(250);
    expect(niceCeiling(4_100)).toBe(5_000);
    expect(niceCeiling(10)).toBe(10);
  });

  test('is 1 for an empty or zero series', () => {
    expect(niceCeiling(0)).toBe(1);
  });
});

describe('niceScale', () => {
  const GiB = 1024 ** 3;
  const MiB = 1024 ** 2;

  test('rounds byte figures in their own binary unit, so the guides fall on whole GB', () => {
    expect(niceScale(19.3 * GiB, 'bytes')).toBe(20 * GiB);
    expect(niceScale(3.3 * GiB, 'bytes')).toBe(5 * GiB);
    expect(niceScale(700 * MiB, 'bytes')).toBe(1000 * MiB);
  });

  test('moves to the next unit when the round figure would pass it, as 1 GB rather than 2000 MB', () => {
    expect(niceScale(1010 * MiB, 'bytes')).toBe(GiB);
  });

  test('rounds other figures in tens', () => {
    expect(niceScale(31.2, 'plain')).toBe(50);
  });
});

describe('guideShares', () => {
  const GiB = 1024 ** 3;

  test('draws quarters when they fall on round figures', () => {
    expect(guideShares(20 * GiB, 'bytes', true)).toEqual([0, 0.25, 0.5, 0.75, 1]);
    expect(guideShares(100, 'plain', true)).toEqual([0, 0.25, 0.5, 0.75, 1]);
  });

  test('draws halves when quarters would not, as 6.25 GB on a 25 GB scale', () => {
    expect(guideShares(25 * GiB, 'bytes', true)).toEqual([0, 0.5, 1]);
    expect(guideShares(50, 'plain', true)).toEqual([0, 0.5, 1]);
  });

  test('draws halves on a short chart', () => {
    expect(guideShares(20 * GiB, 'bytes', false)).toEqual([0, 0.5, 1]);
  });
});

describe('spreadLabels', () => {
  test('leaves labels that are far enough apart where they are', () => {
    expect(spreadLabels([20, 60, 100], 14, 0, 200)).toEqual([20, 60, 100]);
  });

  test('pushes crowded labels apart, keeping their order', () => {
    expect(spreadLabels([100, 104, 108], 14, 0, 200)).toEqual([90, 104, 118]);
  });

  test('keeps every label inside the plot', () => {
    const spread = spreadLabels([195, 197, 199], 14, 0, 200);
    expect(spread).toEqual([172, 186, 200]);
  });
});

describe('nearestPoint', () => {
  test('is the point whose time is nearest the one under the pointer', () => {
    expect(nearestPoint([0, 600, 1800], 1000)).toBe(1);
    expect(nearestPoint([0, 600, 1800], 1300)).toBe(2);
    expect(nearestPoint([0, 600, 1800], -50)).toBe(0);
  });

  test('is none without points', () => {
    expect(nearestPoint([], 10)).toBe(-1);
  });
});
