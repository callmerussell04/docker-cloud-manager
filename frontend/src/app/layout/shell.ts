// The shell's layout rules, by window width. See docs/agent-context/product-frontend.md,
// "Shell".

// The sidebar's widths, labelled and as an icon rail, and its inset from the
// window's edge; PageLayout places the content beside it.
export const sidebarWidth = { expanded: 240, rail: 64 } as const;
export const sidebarInset = 14;

export type SizeClass = 'wide' | 'medium' | 'phone';

// sizeClassFor names the window's size class: wide from 1100 px, medium from
// 700 px, phone below that.
export function sizeClassFor(width: number): SizeClass {
  if (width >= 1100) return 'wide';
  if (width >= 700) return 'medium';
  return 'phone';
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
