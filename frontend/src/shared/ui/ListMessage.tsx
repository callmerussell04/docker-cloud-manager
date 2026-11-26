import type { ReactNode } from 'react';
import { Loader2 } from 'lucide-react';
import { cn } from '@/shared/lib/utils';
import type { StateAction } from './StateMessage';

interface ListMessageProps {
  title: string;
  description?: ReactNode;
  // icon sits above the title, plain: a failure's warning, or the good
  // news of a filter that shows nothing.
  icon?: ReactNode;
  action?: StateAction;
  // primary makes the action the accent button, as the one way to add the
  // first item; otherwise it is a quiet one.
  primary?: boolean;
  role?: 'status' | 'alert';
}

// ListMessage is what a list or a page says in place of its rows, centred:
// a title, one line and one button. Empty lists, lists that could not be
// loaded and searches or filters that leave nothing all use it; a panel's
// own small state uses StateMessage.
export function ListMessage({ title, description, icon, action, primary = false, role }: ListMessageProps) {
  const Icon = action?.busy ? Loader2 : action?.icon;
  return (
    <div role={role} className="grid max-w-[440px] justify-items-center gap-1.5 text-center">
      {icon}
      <b className="text-[17px] font-semibold text-label">{title}</b>
      {description && <span className="text-label-2">{description}</span>}
      {action && (
        <button
          type="button"
          onClick={action.onClick}
          disabled={action.busy}
          className={cn(
            'mt-2.5 inline-flex h-10 items-center gap-1.5 whitespace-nowrap rounded-full font-semibold disabled:opacity-60',
            Icon ? 'pl-3.5 pr-4' : 'px-4',
            primary ? 'bg-accent-fill text-on-accent shadow-[var(--primary-shadow)] hover:brightness-110' : 'bg-fill text-label hover:bg-fill-hover',
          )}
        >
          {Icon && <Icon className={cn('h-4 w-4', action.busy && 'animate-spin')} strokeWidth={primary ? 2.4 : 2.2} />}
          {action.label}
        </button>
      )}
    </div>
  );
}

// ListMessageBelowHead places a ListMessage under a list's column labels,
// for a search or filter that leaves nothing: the labels stay, so the list
// reads as narrowed rather than empty.
export function ListMessageBelowHead(props: ListMessageProps) {
  return (
    <div className="flex justify-center px-4 pt-10">
      <ListMessage {...props} />
    </div>
  );
}
