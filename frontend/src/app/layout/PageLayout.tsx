import { Outlet } from 'react-router-dom';
import { Suspense, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useElementWidth } from '@/shared/lib/hooks';
import { rowsCompact, toolbarStacked } from '@/shared/lib/sizeClass';
import { useSizeClass } from '@/shared/lib/useSizeClass';
import { FrostWindow } from '@/shared/ui/FrostWindow';
import { isPaletteShortcut, onMac } from '@/shared/ui/searchPalette';
import { ShellLayoutContext, type ShellLayout } from '@/shared/ui/shellLayout';
import { PageFallback } from './PageFallback';
import { ShellSearch } from './ShellSearch';
import { Sidebar } from './Sidebar';
import { TabBar } from './TabBar';
import { contentGap, sidebarExpanded, sidebarInset, sidebarWidth, type SidebarChoice } from './shell';

// roundedRect is an SVG path around a box with rounded corners.
function roundedRect(x: number, y: number, width: number, height: number, r: number) {
  const right = x + width;
  const bottom = y + height;
  return `M${x + r} ${y} H${right - r} A${r} ${r} 0 0 1 ${right} ${y + r} V${bottom - r} A${r} ${r} 0 0 1 ${right - r} ${bottom} H${x + r} A${r} ${r} 0 0 1 ${x} ${bottom - r} V${y + r} A${r} ${r} 0 0 1 ${x + r} ${y} Z`;
}

// useSidebarCutout cuts the sidebar's shape out of the window's frost, so the
// sidebar is clearer glass over the wallpaper itself rather than glass over
// frost. It follows both elements as they change size, and leaves the frost
// whole while there is no sidebar (on a phone).
function useSidebarCutout(hasSidebar: boolean) {
  const frostRef = useRef<HTMLDivElement>(null);
  const sidebarRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const frost = frostRef.current;
    const sidebar = sidebarRef.current;
    if (!frost) return;
    if (!hasSidebar || !sidebar) {
      frost.style.clipPath = '';
      return;
    }
    const cut = () => {
      const area = frost.getBoundingClientRect();
      const box = sidebar.getBoundingClientRect();
      const radius = parseFloat(getComputedStyle(sidebar).borderTopLeftRadius) || 0;
      const hole = roundedRect(box.left - area.left, box.top - area.top, box.width, box.height, radius);
      frost.style.clipPath = `path(evenodd, 'M0 0 H${area.width} V${area.height} H0 Z ${hole}')`;
    };
    const observer = new ResizeObserver(cut);
    observer.observe(frost);
    observer.observe(sidebar);
    return () => observer.disconnect();
  }, [hasSidebar]);

  return { frostRef, sidebarRef };
}

// useSearchShortcut opens and closes the search palette with Ctrl K (Cmd K on
// a Mac) from anywhere on a signed-in page, the browser's own use of it aside.
function useSearchShortcut(toggle: () => void) {
  useEffect(() => {
    const mac = onMac();
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isPaletteShortcut(event, mac)) return;
      event.preventDefault();
      toggle();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [toggle]);
}

// PageLayout is the signed-in window: one frosted surface over the wallpaper,
// the sidebar floating through a cut-out in it, and the page beside the
// sidebar. On a phone the tab bar replaces the sidebar. Each page brings its
// own floating toolbar (shared/ui/Page), which, like the page's rows, follows
// the room the content area has. The search palette opens over it all.
export function PageLayout() {
  const sizeClass = useSizeClass();
  const phone = sizeClass === 'phone';
  const [choice, setChoice] = useState<SidebarChoice | null>(null);
  const expanded = sidebarExpanded(sizeClass, choice);
  const { frostRef, sidebarRef } = useSidebarCutout(!phone);
  const contentLeft = phone ? 0 : sidebarInset + (expanded ? sidebarWidth.expanded : sidebarWidth.rail) + contentGap;
  const [searchOpen, setSearchOpen] = useState(false);
  const openSearch = useCallback(() => setSearchOpen(true), []);
  const closeSearch = useCallback(() => setSearchOpen(false), []);
  useSearchShortcut(useCallback(() => setSearchOpen((value) => !value), []));
  const [mainRef, contentWidth] = useElementWidth(window.innerWidth - contentLeft);
  const layout = useMemo<ShellLayout>(
    () => ({ toolbarStacked: toolbarStacked(contentWidth), rowsCompact: rowsCompact(contentWidth), tabBar: phone }),
    [contentWidth, phone],
  );

  return (
    <div className="relative isolate h-screen w-full overflow-hidden">
      <FrostWindow ref={frostRef} className="absolute inset-0 -z-10" />
      {!phone && <Sidebar ref={sidebarRef} expanded={expanded} onToggle={() => setChoice({ sizeClass, expanded: !expanded })} onSearch={openSearch} />}
      <main ref={mainRef} className="absolute inset-y-0 right-0" style={{ left: contentLeft }}>
        <ShellLayoutContext.Provider value={layout}>
          <Suspense fallback={<PageFallback />}>
            <Outlet />
          </Suspense>
        </ShellLayoutContext.Provider>
      </main>
      {phone && <TabBar onSearch={openSearch} />}
      <ShellSearch open={searchOpen} onClose={closeSearch} />
    </div>
  );
}
