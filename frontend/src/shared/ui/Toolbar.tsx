import { useLayoutEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ChevronLeft, Loader2, Plus, Search, X, type LucideIcon } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { useSizeClass } from '@/shared/lib/useSizeClass';
import { cn } from '@/shared/lib/utils';
import { Glass } from './Glass';
import { useShellLayout } from './shellLayout';

interface ToolbarProps {
  title: string;
  // count follows the title, as a list's number of items does.
  count?: number;
  // status follows the title, as a resource's state does.
  status?: ReactNode;
  // leading comes before the title, as a back capsule on a resource page.
  leading?: ReactNode;
  // pills sit in the middle: glass pill groups for filters or tabs.
  pills?: ReactNode;
  // actions are the page's own actions, before the primary action.
  actions?: ReactNode;
  primary?: ReactNode;
}

// The gap between the toolbar's three columns (gap-x-4), and inside the title.
const columnGap = 16;
const titleGap = 8;

// useCrowded reports whether the toolbar's title, pills and actions need more
// room than one row has. Each part is measured at its own natural width,
// which does not change when the pills move to a second row, so the answer
// does not flip back and forth.
function useCrowded(hasPills: boolean) {
  const headerRef = useRef<HTMLElement>(null);
  const titleRef = useRef<HTMLDivElement>(null);
  const pillsRef = useRef<HTMLDivElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const [crowded, setCrowded] = useState(false);

  useLayoutEffect(() => {
    const header = headerRef.current;
    if (!hasPills || !header) return;
    const measure = () => {
      const titleParts = Array.from(titleRef.current?.children ?? []);
      const title = titleParts.reduce((sum, part) => sum + part.scrollWidth, 0) + titleGap * Math.max(titleParts.length - 1, 0);
      const needed = title + (pillsRef.current?.scrollWidth ?? 0) + (endRef.current?.scrollWidth ?? 0) + 2 * columnGap;
      setCrowded(needed > header.clientWidth);
    };
    measure();
    const observer = new ResizeObserver(measure);
    for (const element of [header, titleRef.current, pillsRef.current, endRef.current]) {
      if (element) observer.observe(element);
    }
    return () => observer.disconnect();
  }, [hasPills]);

  return { crowded: hasPills && crowded, headerRef, titleRef, pillsRef, endRef };
}

// Toolbar floats over the top of a page: the title with its count or status,
// optional pills in the middle, then the page's actions and its primary
// action. Rows scroll beneath it (see Page). Only its controls take the
// pointer, so rows stay reachable between them. When the content is narrow,
// or the pills would not fit beside the title and the actions, the pills move
// to a second row, which scrolls sideways, so the title is never squeezed.
export function Toolbar({ title, count, status, leading, pills, actions, primary }: ToolbarProps) {
  const { crowded, headerRef, titleRef, pillsRef, endRef } = useCrowded(pills != null);
  const stacked = useShellLayout().toolbarStacked || crowded;
  const twoRows = stacked && pills != null;
  const pillsContent = (
    <div ref={pillsRef} className="w-max">
      {pills}
    </div>
  );
  return (
    <header
      ref={headerRef}
      data-rows={twoRows ? 2 : 1}
      className={cn(
        'pointer-events-none absolute inset-x-3 top-3.5 z-10 grid items-center gap-x-4',
        stacked ? 'grid-cols-[minmax(0,1fr)_auto]' : 'h-10 grid-cols-[minmax(0,1fr)_auto_minmax(auto,1fr)]',
        twoRows && 'grid-rows-[40px_40px] gap-y-3',
      )}
    >
      <div ref={titleRef} className="pointer-events-auto flex h-10 min-w-0 items-center gap-2">
        {leading}
        <h1 className="truncate text-[20px] font-bold tracking-[-0.022em] text-label">{title}</h1>
        {count !== undefined && <span className="text-[15px] font-medium tabular-nums text-label-3">{count}</span>}
        {status}
      </div>
      {stacked ? (
        pills != null && (
          <div className="pointer-events-auto col-span-2 row-start-2 -mx-3 -my-2 min-w-0 overflow-x-auto px-3 py-2 [scrollbar-width:none]">{pillsContent}</div>
        )
      ) : (
        <div className="pointer-events-auto col-start-2 min-w-0">{pills != null && pillsContent}</div>
      )}
      <div ref={endRef} className={cn('pointer-events-auto flex items-center justify-end gap-2', stacked ? 'col-start-2 row-start-1' : 'col-start-3')}>
        {actions}
        {primary}
      </div>
    </header>
  );
}

interface ToolbarButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> {
  icon: LucideIcon;
  // label is the button's accessible name and tooltip.
  label: string;
  busy?: boolean;
}

// ToolbarButton is a round glass button with only an icon.
export function ToolbarButton({ icon: Icon, label, busy = false, className, disabled, ...props }: ToolbarButtonProps) {
  return (
    <Glass className="shrink-0 rounded-full">
      <button
        type="button"
        aria-label={label}
        title={label}
        disabled={disabled || busy}
        className={cn('flex h-10 w-10 items-center justify-center rounded-full text-label transition-colors hover:bg-lift disabled:opacity-50', className)}
        {...props}
      >
        {busy ? <Loader2 className="h-[17px] w-[17px] animate-spin" /> : <Icon className="h-[17px] w-[17px]" strokeWidth={1.9} />}
      </button>
    </Glass>
  );
}

interface ToolbarPrimaryProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> {
  // icon leads the label; the round phone button always shows a plus.
  icon?: LucideIcon;
  // label is the button's text, and on a phone, where the button is a round
  // plus, its accessible name and tooltip.
  label: string;
}

// ToolbarPrimary is the page's primary action, in the accent. On a phone it
// is a round plus button so the title keeps its room.
export function ToolbarPrimary({ icon: Icon, label, className, ...props }: ToolbarPrimaryProps) {
  const round = useSizeClass() === 'phone';
  return (
    <button
      type="button"
      aria-label={round ? label : undefined}
      title={round ? label : undefined}
      className={cn(
        'inline-flex h-10 shrink-0 items-center whitespace-nowrap rounded-full bg-accent-fill font-semibold text-on-accent',
        'shadow-[var(--primary-shadow)] hover:brightness-110 disabled:opacity-50',
        round ? 'w-10 justify-center' : 'gap-1.5 pl-3 pr-4',
        className,
      )}
      {...props}
    >
      {round ? <Plus className="h-[18px] w-[18px]" strokeWidth={2.4} /> : Icon && <Icon className="h-4 w-4" strokeWidth={2.4} />}
      {!round && label}
    </button>
  );
}

// ToolbarBack is the glass capsule that leads a resource page back to its
// list, labelled with the list's name. On a phone it is a round button with
// the arrow alone, the name its accessible name and tooltip, so the title
// keeps its room.
export function ToolbarBack({ to, label }: { to: string; label: string }) {
  const round = useSizeClass() === 'phone';
  return (
    <Glass className="shrink-0 rounded-full">
      <Link
        to={to}
        aria-label={round ? label : undefined}
        title={round ? label : undefined}
        className={cn(
          'flex h-10 items-center rounded-full font-[550] text-label transition-colors hover:bg-lift',
          round ? 'w-10 justify-center' : 'gap-0.5 pl-2 pr-3.5',
        )}
      >
        <ChevronLeft className="h-[18px] w-[18px]" strokeWidth={2.2} />
        {!round && label}
      </Link>
    </Glass>
  );
}

interface ToolbarSearchProps {
  value: string;
  onChange: (value: string) => void;
  // label is the field's accessible name and tooltip; the placeholder says
  // only "Search".
  label: string;
}

// ToolbarSearch is a glass search field: in the toolbar's middle beside its
// pills, or, on a page with no other pills, first among the actions, where
// it opens toward the title. While it is empty and not in use it is a round
// button, so it takes little room; it opens when pressed and while it holds a
// query, with a button that clears it. Escape clears it too.
export function ToolbarSearch({ value, onChange, label }: ToolbarSearchProps) {
  const t = useT();
  const inputRef = useRef<HTMLInputElement>(null);
  const [focused, setFocused] = useState(false);
  const open = focused || value !== '';
  return (
    <Glass
      className={cn(
        'inline-flex h-10 shrink-0 items-center rounded-full transition-[width]',
        open ? 'w-[200px] gap-1.5 pl-3 pr-1' : 'w-10 justify-center',
        'has-[input:focus-visible]:outline has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-2 has-[input:focus-visible]:outline-accent-fill',
      )}
    >
      {open ? (
        <Search aria-hidden className="h-[15px] w-[15px] shrink-0 text-label-2" strokeWidth={2} />
      ) : (
        <button
          type="button"
          aria-label={label}
          title={label}
          onClick={() => {
            setFocused(true);
            // The field renders on this update; focus it once it is there.
            requestAnimationFrame(() => inputRef.current?.focus());
          }}
          className="flex h-10 w-10 items-center justify-center rounded-full text-label transition-colors hover:bg-lift"
        >
          <Search className="h-[17px] w-[17px]" strokeWidth={1.9} />
        </button>
      )}
      {open && (
        <input
          ref={inputRef}
          type="search"
          value={value}
          aria-label={label}
          title={label}
          placeholder={t('common.search')}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Escape' && value) {
              event.preventDefault();
              onChange('');
            }
          }}
          className="min-w-0 flex-1 bg-transparent text-label placeholder:text-label-3 focus-visible:outline-none [&::-webkit-search-cancel-button]:hidden"
        />
      )}
      {value && (
        <button
          type="button"
          aria-label={t('common.clearSearch')}
          title={t('common.clearSearch')}
          // Keeps focus in the field, so it stays open for the next query.
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => onChange('')}
          className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-label-2 hover:bg-lift hover:text-label"
        >
          <X className="h-[14px] w-[14px]" strokeWidth={2.2} />
        </button>
      )}
    </Glass>
  );
}
