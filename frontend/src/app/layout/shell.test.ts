import { describe, expect, it } from 'vitest';
import { clampSidebarWidth, sidebarExpanded, sidebarWidth, sidebarWidthRange } from './shell';

describe('sidebarExpanded', () => {
  it('starts labelled on wide windows and as an icon rail below them', () => {
    expect(sidebarExpanded('wide', null)).toBe(true);
    expect(sidebarExpanded('medium', null)).toBe(false);
    expect(sidebarExpanded('phone', null)).toBe(false);
  });

  it('keeps the user’s choice while the window stays in the class it was made in', () => {
    expect(sidebarExpanded('wide', { sizeClass: 'wide', expanded: false })).toBe(false);
    expect(sidebarExpanded('medium', { sizeClass: 'medium', expanded: true })).toBe(true);
  });

  it('returns to the class’s default once the window crosses into another class', () => {
    expect(sidebarExpanded('medium', { sizeClass: 'wide', expanded: true })).toBe(false);
    expect(sidebarExpanded('wide', { sizeClass: 'medium', expanded: false })).toBe(true);
  });
});

describe('clampSidebarWidth', () => {
  it('keeps a width inside the range, in whole pixels', () => {
    expect(clampSidebarWidth(287.6)).toBe(288);
    expect(clampSidebarWidth(sidebarWidthRange.min)).toBe(sidebarWidthRange.min);
  });

  it('stops at the narrowest and widest widths', () => {
    expect(clampSidebarWidth(40)).toBe(sidebarWidthRange.min);
    expect(clampSidebarWidth(2000)).toBe(sidebarWidthRange.max);
  });

  it('takes the default for a width that is not a number', () => {
    expect(clampSidebarWidth(Number.NaN)).toBe(sidebarWidth.expanded);
  });
});
