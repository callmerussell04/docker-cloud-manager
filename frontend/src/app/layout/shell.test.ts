import { describe, expect, it } from 'vitest';
import { sidebarExpanded } from './shell';

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
