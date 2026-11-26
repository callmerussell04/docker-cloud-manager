import { describe, expect, it } from 'vitest';
import { ghostBarWidth, ghostRowsMask, gridTracks } from './ghostRows';

describe('gridTracks', () => {
  it('splits a template into its tracks, keeping function arguments together', () => {
    expect(gridTracks('minmax(0,1.5fr) minmax(0,1.2fr) minmax(0,1fr) 96px 124px')).toEqual([
      'minmax(0,1.5fr)', 'minmax(0,1.2fr)', 'minmax(0,1fr)', '96px', '124px',
    ]);
    expect(gridTracks('minmax(0, 1fr) 40px')).toEqual(['minmax(0, 1fr)', '40px']);
    expect(gridTracks('  1fr   2fr ')).toEqual(['1fr', '2fr']);
  });
});

describe('ghostBarWidth', () => {
  it('varies by row and column within a readable range, the same each time', () => {
    const widths = [0, 1, 2, 3].flatMap((row) => [0, 1, 2].map((cell) => ghostBarWidth(row, cell)));
    expect(new Set(widths).size).toBeGreaterThan(4);
    for (const width of widths) {
      expect(width).toBeGreaterThanOrEqual(35);
      expect(width).toBeLessThan(85);
    }
    expect(ghostBarWidth(2, 1)).toBe(ghostBarWidth(2, 1));
  });
});

describe('ghostRowsMask', () => {
  it('fades the rows, and clears an oval behind a message', () => {
    expect(String(ghostRowsMask(false).maskImage)).not.toContain('radial');
    expect(String(ghostRowsMask(true).maskImage)).toContain('radial-gradient');
    expect(ghostRowsMask(true).maskComposite).toBe('intersect');
  });
});
