import { describe, expect, it } from 'vitest';
import { blockContent, memoryMapParts, memoryMapRects, projectSeries, quotaLevel } from './model';

describe('quotaLevel', () => {
  it('is ok below three quarters of the quota', () => {
    expect(quotaLevel(0, 10)).toBe('ok');
    expect(quotaLevel(74, 100)).toBe('ok');
  });

  it('is high from 75% and full from 90%', () => {
    expect(quotaLevel(75, 100)).toBe('high');
    expect(quotaLevel(89, 100)).toBe('high');
    expect(quotaLevel(9, 10)).toBe('full');
    expect(quotaLevel(12, 10)).toBe('full');
  });

  it('is ok when there is no quota to measure against', () => {
    expect(quotaLevel(0, 0)).toBe('ok');
  });
});

describe('memoryMapParts', () => {
  const holder = (id: string, bytes: number) => ({ id, bytes });

  it('puts the largest reservations first and the free part last, together filling the quota', () => {
    const parts = memoryMapParts([holder('web', 512), holder('db', 2048), holder('cache', 1024)], 3584, 4096);
    expect(parts.map((part) => [part.kind, part.kind === 'holder' ? part.holder.id : '', part.bytes])).toEqual([
      ['holder', 'db', 2048],
      ['holder', 'cache', 1024],
      ['holder', 'web', 512],
      ['free', '', 512],
    ]);
  });

  it('shows reservations the list does not name as one other part', () => {
    // 3 GiB reserved, but only 2 GiB of it is on the page that was read.
    const parts = memoryMapParts([holder('db', 2048)], 3072, 4096);
    expect(parts.map((part) => [part.kind, part.bytes])).toEqual([
      ['holder', 2048],
      ['other', 1024],
      ['free', 1024],
    ]);
  });

  it('leaves nothing free when the reservations reach or pass the quota', () => {
    const parts = memoryMapParts([holder('a', 3072), holder('b', 2048)], 5120, 4096);
    expect(parts.map((part) => part.kind)).toEqual(['holder', 'holder']);
  });

  it('is all free with nothing reserved, and drops holders that reserve nothing', () => {
    expect(memoryMapParts([holder('idle', 0)], 0, 4096)).toEqual([{ kind: 'free', bytes: 4096 }]);
  });

  it('keeps equal reservations in a steady order, by id', () => {
    const parts = memoryMapParts([holder('b', 256), holder('a', 256)], 512, 512);
    expect(parts.map((part) => (part.kind === 'holder' ? part.holder.id : part.kind))).toEqual(['a', 'b']);
  });
});

describe('blockContent', () => {
  it('shows the name and amount when the block has room for both', () => {
    expect(blockContent(220, 120, 'storefront')).toBe('name');
  });

  it('shows only the amount when the name would not fit across or the block is too low for two lines', () => {
    expect(blockContent(90, 120, 'storefront-worker')).toBe('amount');
    expect(blockContent(220, 40, 'web')).toBe('amount');
  });

  it('lets a long name truncate once the block is wide enough for a readable part of it', () => {
    expect(blockContent(160, 120, 'a-very-long-container-name-indeed')).toBe('name');
  });

  it('shows nothing in a block too narrow or low for the amount, such as "128 MB"', () => {
    expect(blockContent(60, 120, 'web')).toBe('none');
    expect(blockContent(120, 20, 'web')).toBe('none');
  });
});

describe('projectSeries', () => {
  it('gives each project a chart series by a fixed order, whatever order they are listed in', () => {
    const first = projectSeries(['p-b', 'p-a', 'p-c']);
    const again = projectSeries(['p-c', 'p-a', 'p-b', 'p-a']);
    expect([...first]).toEqual([['p-a', 1], ['p-b', 2], ['p-c', 3]]);
    expect(again).toEqual(first);
  });

  it('starts over at the first series after the fifth', () => {
    const series = projectSeries(['1', '2', '3', '4', '5', '6']);
    expect(series.get('5')).toBe(5);
    expect(series.get('6')).toBe(1);
  });
});

describe('memoryMapRects', () => {
  const MiB = 1024 * 1024;
  // The dashboard's map: containers from 1 GiB down to 64 MiB, then 12.3 GiB free.
  const bytes = [1024, 768, 512, 512, 384, 256, 256, 64, 12595].map((value) => value * MiB);
  const free = bytes.length - 1;
  const overlap = (a: { x: number; y: number; width: number; height: number }, b: typeof a) =>
    Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x) > 0.01 && Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y) > 0.01;

  it.each([[1100, 440], [800, 440], [420, 440], [1600, 300]])('keeps every block apart, inside the map and at least 20 px thick at %i × %i', (width, height) => {
    const rects = memoryMapRects(bytes, free, width, height);
    for (let i = 0; i < rects.length; i++) {
      const rect = rects[i];
      expect(rect.x).toBeGreaterThanOrEqual(-0.01);
      expect(rect.y).toBeGreaterThanOrEqual(-0.01);
      expect(rect.x + rect.width).toBeLessThanOrEqual(width + 0.01);
      expect(rect.y + rect.height).toBeLessThanOrEqual(height + 0.01);
      if (i !== free) expect(Math.min(rect.width, rect.height)).toBeGreaterThanOrEqual(19.99);
      for (let j = i + 1; j < rects.length; j++) expect(overlap(rect, rects[j])).toBe(false);
    }
  });

  it('gives the free part its own region along the longer side', () => {
    const rects = memoryMapRects(bytes, free, 1100, 440);
    const heldRight = Math.max(...rects.slice(0, free).map((rect) => rect.x + rect.width));
    expect(rects[free].x).toBeCloseTo(heldRight, 5);
    expect(rects[free].height).toBe(440);
  });

  it('keeps larger reservations larger', () => {
    const rects = memoryMapRects(bytes, free, 1100, 440);
    const area = (index: number) => rects[index].width * rects[index].height;
    expect(area(0)).toBeGreaterThan(area(1));
    expect(area(1)).toBeGreaterThan(area(4));
  });

  it('fills the map with the blocks when nothing is free, and with the free part when nothing is held', () => {
    const full = memoryMapRects([300, 100], -1, 400, 200);
    expect(full[0].width * full[0].height + full[1].width * full[1].height).toBeCloseTo(400 * 200, 3);
    expect(memoryMapRects([0, 500], 1, 400, 200)[1]).toEqual({ x: 0, y: 0, width: 400, height: 200 });
  });
});
