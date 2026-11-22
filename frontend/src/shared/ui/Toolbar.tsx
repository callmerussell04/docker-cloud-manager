import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ChevronLeft, Loader2, Plus, type LucideIcon } from 'lucide-react';
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

// Toolbar floats over the top of a page: the title with its count or status,
// optional pills in the middle, then the page's actions and its primary
// action. Rows scroll beneath it (see Page). Only its controls take the
// pointer, so rows stay reachable between them. When the content is narrow
// the pills move to a second row, which scrolls sideways, so the title is
// never squeezed.
export function Toolbar({ title, count, status, leading, pills, actions, primary }: ToolbarProps) {
  const stacked = useShellLayout().toolbarStacked;
  const twoRows = stacked && pills != null;
  return (
    <header
      data-rows={twoRows ? 2 : 1}
      className={cn(
        'pointer-events-none absolute inset-x-3 top-3.5 z-10 grid items-center gap-x-4',
        stacked ? 'grid-cols-[minmax(0,1fr)_auto]' : 'h-10 grid-cols-[minmax(0,1fr)_auto_minmax(auto,1fr)]',
        twoRows && 'grid-rows-[40px_40px] gap-y-3',
      )}
    >
      <div className="pointer-events-auto flex h-10 min-w-0 items-center gap-2">
        {leading}
        <h1 className="truncate text-[20px] font-bold tracking-[-0.022em] text-label">{title}</h1>
        {count !== undefined && <span className="text-[15px] font-medium tabular-nums text-label-3">{count}</span>}
        {status}
      </div>
      {stacked ? (
        pills != null && (
          <div className="pointer-events-auto col-span-2 row-start-2 -mx-3 -my-2 min-w-0 overflow-x-auto px-3 py-2 [scrollbar-width:none]">{pills}</div>
        )
      ) : (
        <div className="pointer-events-auto col-start-2 min-w-0">{pills}</div>
      )}
      <div className={cn('pointer-events-auto flex items-center justify-end gap-2', stacked ? 'col-start-2 row-start-1' : 'col-start-3')}>
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
// list, labelled with the list's name.
export function ToolbarBack({ to, label }: { to: string; label: string }) {
  return (
    <Glass className="shrink-0 rounded-full">
      <Link to={to} className="flex h-10 items-center gap-0.5 rounded-full pl-2 pr-3.5 font-[550] text-label transition-colors hover:bg-lift">
        <ChevronLeft className="h-[18px] w-[18px]" strokeWidth={2.2} />
        {label}
      </Link>
    </Glass>
  );
}
