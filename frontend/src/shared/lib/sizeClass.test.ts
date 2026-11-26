import { describe, expect, it } from 'vitest';
import { rowsCompact, sizeClassFor, toolbarStacked } from './sizeClass';

describe('sizeClassFor', () => {
  it.each([
    [1440, 'wide'],
    [1100, 'wide'],
    [1099, 'medium'],
    [768, 'medium'],
    [700, 'medium'],
    [699, 'phone'],
    [390, 'phone'],
  ] as const)('puts a %i px window in the %s class', (width, expected) => {
    expect(sizeClassFor(width)).toBe(expected);
  });
});

describe('toolbarStacked', () => {
  it.each([
    [1200, false],
    [640, false],
    [639, true],
    [390, true],
  ] as const)('with %i px of content, stacks the pills on a second row: %s', (width, expected) => {
    expect(toolbarStacked(width)).toBe(expected);
  });
});

describe('rowsCompact', () => {
  it.each([
    [1200, false],
    [600, false],
    [599, true],
    [390, true],
  ] as const)('with %i px of content, shows two-line rows: %s', (width, expected) => {
    expect(rowsCompact(width)).toBe(expected);
  });

  it('follows the content, not the window: a medium window with the sidebar open can leave phone-width content', () => {
    // 768 px window, 254 px taken by the labelled sidebar and its inset.
    expect(sizeClassFor(768)).toBe('medium');
    expect(rowsCompact(768 - 254)).toBe(true);
    expect(toolbarStacked(768 - 254)).toBe(true);
  });
});
