// The shell's sidebar rules. The size classes and content-width rules are in
// shared/lib/sizeClass.ts. See docs/agent-context/product-frontend.md, "Shell".

import type { SizeClass } from '@/shared/lib/sizeClass';

// The sidebar's widths, labelled and as an icon rail, and its inset from the
// window's edge; PageLayout places the content beside it, contentGap further
// on, so a page's title and rows keep clear of the sidebar's glass.
export const sidebarWidth = { expanded: 240, rail: 64 } as const;
export const sidebarInset = 14;
export const contentGap = 14;

// The labelled sidebar's width can be dragged between these bounds; the rail
// keeps its own. sidebarWidthStep is how far an arrow key moves it.
export const sidebarWidthRange = { min: 200, max: 360 } as const;
export const sidebarWidthStep = 16;

// clampSidebarWidth keeps a dragged or stored width within the range, as a
// whole number of pixels; anything that is not a number is the default.
export function clampSidebarWidth(width: number): number {
  if (!Number.isFinite(width)) return sidebarWidth.expanded;
  return Math.round(Math.min(Math.max(width, sidebarWidthRange.min), sidebarWidthRange.max));
}

// SidebarChoice is the user's last collapse or expand, and the size class the
// window was in when they made it.
export interface SidebarChoice {
  sizeClass: SizeClass;
  expanded: boolean;
}

// sidebarExpanded says whether the sidebar shows labelled or as an icon rail.
// It starts labelled only on wide windows; the user's choice holds until the
// window crosses into another size class, which resets it to that class's
// default.
export function sidebarExpanded(sizeClass: SizeClass, choice: SidebarChoice | null): boolean {
  if (choice && choice.sizeClass === sizeClass) return choice.expanded;
  return sizeClass === 'wide';
}
