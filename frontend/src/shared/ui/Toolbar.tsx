import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ChevronLeft, Loader2, type LucideIcon } from 'lucide-react';
import { cn } from '@/shared/lib/utils';
import { Glass } from './Glass';

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
// pointer, so rows stay reachable between them.
export function Toolbar({ title, count, status, leading, pills, actions, primary }: ToolbarProps) {
  return (
    <header className="pointer-events-none absolute inset-x-3 top-3.5 z-10 grid h-10 grid-cols-[minmax(0,1fr)_auto_minmax(auto,1fr)] items-center gap-4">
      <div className="pointer-events-auto flex min-w-0 items-center gap-2">
        {leading}
        <h1 className="truncate text-[20px] font-bold tracking-[-0.022em] text-label">{title}</h1>
        {count !== undefined && <span className="text-[15px] font-medium tabular-nums text-label-3">{count}</span>}
        {status}
      </div>
      <div className="pointer-events-auto col-start-2 min-w-0">{pills}</div>
      <div className="pointer-events-auto col-start-3 flex items-center justify-end gap-2">
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

interface ToolbarPrimaryProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon?: LucideIcon;
}

// ToolbarPrimary is the page's primary action, in the accent.
export function ToolbarPrimary({ icon: Icon, children, className, ...props }: ToolbarPrimaryProps) {
  return (
    <button
      type="button"
      className={cn(
        'inline-flex h-10 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full bg-accent-fill pl-3 pr-4 font-semibold text-on-accent',
        'shadow-[var(--primary-shadow)] hover:brightness-110 disabled:opacity-50',
        className,
      )}
      {...props}
    >
      {Icon && <Icon className="h-4 w-4" strokeWidth={2.4} />}
      {children}
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
