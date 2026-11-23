import { describe, expect, it } from 'vitest';
import { treemap } from './treemap';

const area = (rect: { width: number; height: number }) => rect.width * rect.height;

describe('treemap', () => {
  it('gives a single size the whole rectangle', () => {
    expect(treemap([5], 300, 200)).toEqual([{ x: 0, y: 0, width: 300, height: 200 }]);
  });

  it('splits a rectangle in proportion to the sizes, in the order given', () => {
    // 6 + 3 + 3 = 12 over 400 x 300 = 120000: 60000, 30000, 30000.
    const rects = treemap([6, 3, 3], 400, 300);
    expect(rects.map(area)).toEqual([60000, 30000, 30000].map((value) => expect.closeTo(value, 6)));
  });

  it('fills the rectangle exactly, without overlap or overflow', () => {
    const sizes = [9, 7, 5, 4, 2, 1, 1, 0.5];
    const rects = treemap(sizes, 640, 360);
    expect(rects.reduce((sum, rect) => sum + area(rect), 0)).toBeCloseTo(640 * 360, 6);
    for (const rect of rects) {
      expect(rect.x).toBeGreaterThanOrEqual(-1e-9);
      expect(rect.y).toBeGreaterThanOrEqual(-1e-9);
      expect(rect.x + rect.width).toBeLessThanOrEqual(640 + 1e-9);
      expect(rect.y + rect.height).toBeLessThanOrEqual(360 + 1e-9);
    }
    for (let a = 0; a < rects.length; a++) {
      for (let b = a + 1; b < rects.length; b++) {
        const overlapX = Math.min(rects[a].x + rects[a].width, rects[b].x + rects[b].width) - Math.max(rects[a].x, rects[b].x);
        const overlapY = Math.min(rects[a].y + rects[a].height, rects[b].y + rects[b].height) - Math.max(rects[a].y, rects[b].y);
        expect(overlapX > 1e-9 && overlapY > 1e-9).toBe(false);
      }
    }
  });

  it('keeps blocks close to square rather than slicing thin strips', () => {
    // Four equal sizes in a square make four squares, not four strips.
    const rects = treemap([1, 1, 1, 1], 200, 200);
    for (const rect of rects) {
      expect(rect.width).toBeCloseTo(100, 6);
      expect(rect.height).toBeCloseTo(100, 6);
    }
  });

  it('gives a zero size an empty rectangle and nothing for an empty whole', () => {
    expect(treemap([], 100, 100)).toEqual([]);
    expect(treemap([0, 0], 100, 100).map(area)).toEqual([0, 0]);
    expect(area(treemap([4, 0], 100, 100)[1])).toBe(0);
  });
});
