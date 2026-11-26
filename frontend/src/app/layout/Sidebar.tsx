import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type Ref } from 'react';
import { NavLink } from 'react-router-dom';
import { PanelLeftClose, PanelLeftOpen, Search } from 'lucide-react';
import { useAuthStore } from '@/shared/api/sessionStore';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Glass } from '@/shared/ui/Glass';
import { Kbd } from '@/shared/ui/Kbd';
import { paletteShortcut } from '@/shared/ui/searchPalette';
import { AccountMenu, PreferenceButtons } from './AccountMenu';
import { adminNavigation, navigation, type NavEntry } from './navigation';
import { clampSidebarWidth, sidebarInset, sidebarWidth, sidebarWidthRange, sidebarWidthStep } from './shell';

// The product's initials, shown where its name (app.name) does not fit.
const productShortName = 'DCM';

// Brand is the product's name in the sidebar's brand row: in full when it
// fits beside the collapse button, otherwise its initials. The full name is
// measured in a hidden copy, so the choice does not flip as it changes.
function Brand() {
  const productName = useT()('app.name');
  const boxRef = useRef<HTMLSpanElement>(null);
  const fullRef = useRef<HTMLSpanElement>(null);
  const [fits, setFits] = useState(true);

  useLayoutEffect(() => {
    const box = boxRef.current;
    const full = fullRef.current;
    if (!box || !full) return;
    const measure = () => setFits(full.offsetWidth <= box.clientWidth);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(box);
    return () => observer.disconnect();
  }, []);

  return (
    <span ref={boxRef} className="relative min-w-0 flex-1 overflow-hidden whitespace-nowrap text-[14.5px] font-bold tracking-[-0.02em] text-label" title={fits ? undefined : productName}>
      {fits ? productName : <><span aria-hidden>{productShortName}</span><span className="sr-only">{productName}</span></>}
      <span ref={fullRef} aria-hidden className="invisible absolute left-0 top-0">{productName}</span>
    </span>
  );
}

function NavGroup({ items, compact }: { items: NavEntry[]; compact: boolean }) {
  const t = useT();
  return (
    <ul className="grid gap-px">
      {items.map((item) => {
        const name = t(item.nameKey);
        return (
          <li key={item.href}>
            <NavLink
              to={item.href}
              end={item.href === '/'}
              className={cn('nav-item', compact && 'h-10 justify-center px-0')}
              aria-label={compact ? name : undefined}
              title={compact ? name : undefined}
            >
              <item.icon className={compact ? 'h-[18px] w-[18px]' : 'h-4 w-4'} strokeWidth={1.8} />
              {!compact && name}
            </NavLink>
          </li>
        );
      })}
    </ul>
  );
}

// ResizeHandle is the labelled sidebar's right edge: dragged, it sets the
// sidebar's width within its range; arrow keys move it a step, Home and End
// to either end, and a double click returns it to the default.
function ResizeHandle({ width, onResize, onResizeEnd }: Pick<SidebarProps, 'width' | 'onResize' | 'onResizeEnd'>) {
  const t = useT();
  const drag = useRef<{ startX: number; startWidth: number; width: number } | null>(null);
  const [dragging, setDragging] = useState(false);

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = { startX: event.clientX, startWidth: width, width };
    setDragging(true);
  };
  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    drag.current.width = clampSidebarWidth(drag.current.startWidth + event.clientX - drag.current.startX);
    onResize(drag.current.width);
  };
  const endDrag = () => {
    if (!drag.current) return;
    onResizeEnd(drag.current.width);
    drag.current = null;
    setDragging(false);
  };
  const set = (next: number) => {
    const clamped = clampSidebarWidth(next);
    onResize(clamped);
    onResizeEnd(clamped);
  };
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const next = {
      ArrowLeft: width - sidebarWidthStep,
      ArrowRight: width + sidebarWidthStep,
      Home: sidebarWidthRange.min,
      End: sidebarWidthRange.max,
    }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    set(next);
  };

  // While dragging, the whole window keeps the resize cursor and no text is
  // selected as the pointer passes over it.
  useEffect(() => {
    if (!dragging) return;
    const { cursor, userSelect } = document.body.style;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    return () => {
      document.body.style.cursor = cursor;
      document.body.style.userSelect = userSelect;
    };
  }, [dragging]);

  const label = t('shell.resizeSidebar');
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      title={label}
      aria-valuemin={sidebarWidthRange.min}
      aria-valuemax={sidebarWidthRange.max}
      aria-valuenow={width}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onDoubleClick={() => set(sidebarWidth.expanded)}
      onKeyDown={onKeyDown}
      className="group absolute -right-[7px] bottom-6 top-6 z-10 flex w-3.5 cursor-col-resize touch-none items-center justify-center outline-none"
    >
      <span
        aria-hidden
        className={cn(
          'h-10 w-[3px] rounded-full bg-label-3 transition-opacity',
          dragging ? 'opacity-70' : 'opacity-0 group-hover:opacity-50 group-focus-visible:opacity-70',
        )}
      />
    </div>
  );
}

interface SidebarProps {
  expanded: boolean;
  // width is the labelled sidebar's width; onResize follows a drag of its
  // edge, and onResizeEnd takes the width the drag ended on.
  width: number;
  onResize: (width: number) => void;
  onResizeEnd: (width: number) => void;
  onToggle: () => void;
  // onSearch opens the search palette.
  onSearch: () => void;
  ref?: Ref<HTMLDivElement>;
}

// Sidebar floats as clearer glass through a cut-out in the window's frost:
// a search field that opens the search palette, the pages, then
// Administration for admins, and the account at its foot.
// Labelled, it has a collapse button that turns it into an icon rail, and an
// edge that is dragged to widen or narrow it; the rail's expand button turns
// it back.
export function Sidebar({ expanded, width, onResize, onResizeEnd, onToggle, onSearch, ref }: SidebarProps) {
  const t = useT();
  const isAdmin = useAuthStore((state) => state.role) === 'admin';
  const compact = !expanded;
  const productName = t('app.name');
  const toggleLabel = expanded ? t('shell.collapseSidebar') : t('shell.expandSidebar');

  // The toggle button moves between the brand row and the rail when it is
  // used, so focus follows it to its new place.
  const toggleRef = useRef<HTMLButtonElement>(null);
  const toggled = useRef(false);
  useEffect(() => {
    if (!toggled.current) return;
    toggled.current = false;
    toggleRef.current?.focus();
  }, [expanded]);

  const toggle = (
    <button
      ref={toggleRef}
      type="button"
      className={cn('grid shrink-0 place-items-center rounded-lg text-label-2 transition-colors hover:bg-fill-hover hover:text-label', compact ? 'h-9 w-full' : 'h-7 w-7')}
      aria-label={toggleLabel}
      title={toggleLabel}
      aria-expanded={expanded}
      onClick={() => {
        toggled.current = true;
        onToggle();
      }}
    >
      {expanded ? <PanelLeftClose className="h-4 w-4" strokeWidth={1.8} /> : <PanelLeftOpen className="h-[18px] w-[18px]" strokeWidth={1.8} />}
    </button>
  );

  const searchLabel = t('search.open');
  const shortcut = paletteShortcut();
  const search = (
    <button
      type="button"
      className={cn(
        'mb-3 flex shrink-0 items-center gap-[7px] rounded-[9px] bg-fill text-label-2 transition-colors hover:bg-fill-hover',
        compact ? 'h-10 w-full justify-center' : 'h-[30px] px-2',
      )}
      aria-label={searchLabel}
      aria-keyshortcuts={shortcut.aria}
      title={`${searchLabel} (${shortcut.label})`}
      onClick={onSearch}
    >
      <Search className={compact ? 'h-[18px] w-[18px]' : 'h-3.5 w-3.5'} strokeWidth={2} />
      {!compact && (
        <>
          <span className="flex-1 text-left">{searchLabel}</span>
          <Kbd>{shortcut.label}</Kbd>
        </>
      )}
    </button>
  );

  return (
    <Glass
      ref={ref}
      material="sidebar"
      className="absolute z-20 rounded-[24px]"
      style={{ top: sidebarInset, bottom: sidebarInset, left: sidebarInset, width: expanded ? width : sidebarWidth.rail }}
    >
      <nav aria-label={t('shell.navigation')} className={cn('flex h-full flex-col overflow-y-auto', compact ? 'px-2 py-3.5' : 'p-3.5')}>
        <div className={cn('flex items-center gap-2', compact ? 'justify-center pb-2.5 pt-1' : 'px-2 pb-3 pt-1')}>
          {compact ? (
            <span className="text-[13px] font-bold tracking-[-0.02em] text-label" title={productName}>
              <span aria-hidden>{productShortName}</span>
              <span className="sr-only">{productName}</span>
            </span>
          ) : (
            <Brand />
          )}
          {expanded && toggle}
        </div>
        {compact && <div className="mb-2.5">{toggle}</div>}
        {search}

        <NavGroup items={navigation} compact={compact} />

        {isAdmin && (
          <>
            {compact ? (
              <div role="separator" aria-label={t('common.admin')} className="mx-2.5 my-3 h-px bg-separator" />
            ) : (
              <div className="px-[9px] pb-1.5 pt-[18px] text-[11px] font-semibold text-label-2">{t('common.admin')}</div>
            )}
            <NavGroup items={adminNavigation} compact={compact} />
          </>
        )}

        <div className="min-h-4 flex-1" />
        {compact ? (
          <div className="flex flex-col items-center gap-1">
            <PreferenceButtons />
            <AccountMenu compact />
          </div>
        ) : (
          <div className="flex items-center gap-0.5">
            <div className="min-w-0 flex-1"><AccountMenu compact={false} /></div>
            <PreferenceButtons />
          </div>
        )}
      </nav>
      {expanded && <ResizeHandle width={width} onResize={onResize} onResizeEnd={onResizeEnd} />}
    </Glass>
  );
}
