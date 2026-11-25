import { describe, expect, it } from 'vitest';
import { lensDisplacement, rimWeight, supportsLensRefraction } from './lens';

describe('lensDisplacement', () => {
  // A 200 × 100 rectangle with 20 px corners and a 10 px bezel.
  const box = { width: 200, height: 100, radius: 20, bezel: 10 };

  it('leaves the flat middle of the glass undisturbed', () => {
    expect(lensDisplacement(100, 50, box)).toEqual({ x: 0, y: 0 });
    expect(lensDisplacement(30, 50, box)).toEqual({ x: 0, y: 0 });
  });

  it('samples the backdrop from further inside at each edge, so it bends inward', () => {
    const left = lensDisplacement(0, 50, box);
    const right = lensDisplacement(199, 50, box);
    const top = lensDisplacement(100, 0, box);
    const bottom = lensDisplacement(100, 99, box);
    expect(left.x).toBeGreaterThan(0);
    expect(right.x).toBeLessThan(0);
    expect(top.y).toBeGreaterThan(0);
    expect(bottom.y).toBeLessThan(0);
    expect([left.y, right.y, top.x, bottom.x]).toEqual([0, 0, 0, 0]);
  });

  it('bends most at the rim and less toward the inner edge of the bezel', () => {
    const rim = lensDisplacement(0, 50, box).x;
    const inner = lensDisplacement(8, 50, box).x;
    expect(rim).toBeGreaterThan(inner);
    expect(inner).toBeGreaterThan(0);
    expect(rim).toBeLessThanOrEqual(1);
  });

  it('bends diagonally in a rounded corner', () => {
    const corner = lensDisplacement(6, 6, box);
    expect(corner.x).toBeGreaterThan(0);
    expect(corner.y).toBeGreaterThan(0);
    expect(corner.x).toBeCloseTo(corner.y, 5);
  });

  it('leaves the backdrop outside the rounded corner alone', () => {
    expect(lensDisplacement(0, 0, box)).toEqual({ x: 0, y: 0 });
  });
});

describe('rimWeight', () => {
  // A 200 × 100 rectangle with 20 px corners and a 10 px bezel.
  const box = { width: 200, height: 100, radius: 20, bezel: 10 };

  it('shows the sharp rim across the whole bezel', () => {
    expect(rimWeight(0, 50, box)).toBe(1);
    expect(rimWeight(9, 50, box)).toBe(1);
    expect(rimWeight(100, 0, box)).toBe(1);
  });

  it('fades into the blurred middle just past the bezel', () => {
    const fading = rimWeight(12, 50, box);
    expect(fading).toBeGreaterThan(0);
    expect(fading).toBeLessThan(1);
    expect(rimWeight(40, 50, box)).toBe(0);
    expect(rimWeight(100, 50, box)).toBe(0);
  });
});

describe('supportsLensRefraction', () => {
  it('accepts Chromium browsers, which take an SVG filter in backdrop-filter', () => {
    expect(supportsLensRefraction('Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36')).toBe(true);
    expect(supportsLensRefraction('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36 Edg/142.0.0.0')).toBe(true);
    expect(supportsLensRefraction('Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/142.0.0.0 Safari/537.36')).toBe(true);
  });

  it('refuses Safari, Firefox and iOS browsers, which fall back to blur', () => {
    expect(supportsLensRefraction('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15')).toBe(false);
    expect(supportsLensRefraction('Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0')).toBe(false);
    expect(supportsLensRefraction('Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/142.0.0.0 Mobile/15E148 Safari/604.1')).toBe(false);
  });
});
