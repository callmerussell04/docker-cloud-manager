import type { ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';
import { Loader2 } from 'lucide-react';
import { cn } from '@/shared/lib/utils';

export interface StateAction {
  label: string;
  onClick: () => void;
  icon?: LucideIcon;
  busy?: boolean;
}

interface StateMessageProps {
  title: string;
  description?: ReactNode;
  action?: StateAction;
  // bad marks a failure: the title is red.
  tone?: 'plain' | 'bad';
  role?: 'status' | 'alert';
}

// StateMessage is the one quiet shape every shared state takes: a bold line,
// one line of explanation and one text action, on the frost with no icon or
// panel.
export function StateMessage({ title, description, action, tone = 'plain', role }: StateMessageProps) {
  const Icon = action?.busy ? Loader2 : action?.icon;
  return (
    <div role={role} className="grid justify-items-start gap-1 px-1.5 py-6">
      <b className={cn('text-[15px] font-semibold', tone === 'bad' && 'text-red-text')}>{title}</b>
      {description && <span className="text-label-2">{description}</span>}
      {action && (
        <button
          type="button"
          onClick={action.onClick}
          disabled={action.busy}
          className="mt-1.5 inline-flex items-center gap-1 whitespace-nowrap rounded-md font-medium text-accent hover:underline disabled:opacity-60"
        >
          {Icon && <Icon className={cn('h-3.5 w-3.5', action.busy && 'animate-spin')} />}
          {action.label}
        </button>
      )}
    </div>
  );
}
