// The layout rules every screen follows, by width. See
// docs/agent-context/product-frontend.md, "Shell".

export type SizeClass = 'wide' | 'medium' | 'phone';

// sizeClassFor names the window's size class: wide from 1100 px, medium from
// 700 px, phone below that.
export function sizeClassFor(windowWidth: number): SizeClass {
  if (windowWidth >= 1100) return 'wide';
  if (windowWidth >= 700) return 'medium';
  return 'phone';
}

// The next two rules follow the room the content area has, not the window: a
// labelled sidebar on a medium window can leave the content as narrow as a
// phone's.

// toolbarStacked says whether the toolbar moves its pills to a second row,
// below 640 px of content, so the title is never squeezed.
export function toolbarStacked(contentWidth: number): boolean {
  return contentWidth < 640;
}

// rowsCompact says whether list rows become two lines (the name, then status
// and age), below 600 px of content.
export function rowsCompact(contentWidth: number): boolean {
  return contentWidth < 600;
}
