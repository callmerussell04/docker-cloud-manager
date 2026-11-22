import { Outlet } from 'react-router-dom';
import { Suspense, useLayoutEffect, useRef, useState } from 'react';
import { FrostWindow } from '@/shared/ui/FrostWindow';
import { PageFallback } from './PageFallback';
import { Sidebar } from './Sidebar';
import { sidebarExpanded, sidebarInset, sidebarWidth, type SidebarChoice } from './shell';
import { useSizeClass } from './useSizeClass';

// roundedRect is an SVG path around a box with rounded corners.
function roundedRect(x: number, y: number, width: number, height: number, r: number) {
  const right = x + width;
  const bottom = y + height;
  return `M${x + r} ${y} H${right - r} A${r} ${r} 0 0 1 ${right} ${y + r} V${bottom - r} A${r} ${r} 0 0 1 ${right - r} ${bottom} H${x + r} A${r} ${r} 0 0 1 ${x} ${bottom - r} V${y + r} A${r} ${r} 0 0 1 ${x + r} ${y} Z`;
}

// useSidebarCutout cuts the sidebar's shape out of the window's frost, so the
// sidebar is clearer glass over the wallpaper itself rather than glass over
// frost. It follows both elements as they change size.
function useSidebarCutout() {
  const frostRef = useRef<HTMLDivElement>(null);
  const sidebarRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const frost = frostRef.current;
    const sidebar = sidebarRef.current;
    if (!frost || !sidebar) return;
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
  }, []);

  return { frostRef, sidebarRef };
}

// PageLayout is the signed-in window: one frosted surface over the wallpaper,
// the sidebar floating through a cut-out in it, and the page beside the
// sidebar. Each page brings its own floating toolbar (shared/ui/Page).
export function PageLayout() {
  const sizeClass = useSizeClass();
  const [choice, setChoice] = useState<SidebarChoice | null>(null);
  const expanded = sidebarExpanded(sizeClass, choice);
  const { frostRef, sidebarRef } = useSidebarCutout();
  const contentLeft = sidebarInset + (expanded ? sidebarWidth.expanded : sidebarWidth.rail);

  return (
    <div className="relative isolate h-screen w-full overflow-hidden">
      <FrostWindow ref={frostRef} className="absolute inset-0 -z-10" />
      <Sidebar ref={sidebarRef} expanded={expanded} onToggle={() => setChoice({ sizeClass, expanded: !expanded })} />
      <main className="absolute inset-y-0 right-0" style={{ left: contentLeft }}>
        <Suspense fallback={<PageFallback />}>
          <Outlet />
        </Suspense>
      </main>
    </div>
  );
}
