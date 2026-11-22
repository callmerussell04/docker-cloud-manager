// The shell's sidebar rules. The size classes and content-width rules are in
// shared/lib/sizeClass.ts. See docs/agent-context/product-frontend.md, "Shell".

import type { SizeClass } from '@/shared/lib/sizeClass';

// The sidebar's widths, labelled and as an icon rail, and its inset from the
// window's edge; PageLayout places the content beside it.
export const sidebarWidth = { expanded: 240, rail: 64 } as const;
export const sidebarInset = 14;

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
