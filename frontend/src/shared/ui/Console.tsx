import type { ButtonHTMLAttributes, ReactNode } from 'react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/shared/lib/utils';

// Console is the dark surface logs and the terminal sit on, dark in both
// themes. It fills the room its parent gives it: a bar on top, a body below.
export function Console({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cn(
        'relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-[18px] bg-console font-mono text-[12.5px] text-console-label',
        'shadow-[var(--console-shadow)]',
        className,
      )}
    >
      {children}
    </div>
  );
}

// ConsoleBar is the row of the console's own controls above its body.
export function ConsoleBar({ children }: { children: ReactNode }) {
  return (
    <div className="flex h-11 flex-none items-center gap-1.5 border-b border-console-fill pl-4 pr-2.5 font-sans">
      {children}
    </div>
  );
}

// ConsoleState says what the console is showing: live (a green dot), or
// anything else, such as an ended stream, with no dot. The label always comes
// with it.
export function ConsoleState({ live, children }: { live: boolean; children: ReactNode }) {
  return (
    <span role="status" className="mr-2 inline-flex shrink-0 items-center gap-[7px] text-[12px] font-semibold text-console-label-2">
      {live && <span aria-hidden className="h-[7px] w-[7px] shrink-0 rounded-full bg-green shadow-[0_0_0_3px_color-mix(in_srgb,var(--green)_18%,transparent)]" />}
      <span className="whitespace-nowrap">{children}</span>
    </span>
  );
}

interface ConsoleButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> {
  // label is the button's text, or with an icon alone its accessible name
  // and tooltip.
  label: string;
  icon?: LucideIcon;
  // iconOnly shows the icon without the label.
  iconOnly?: boolean;
  // pressed makes the button a toggle, lit while on.
  pressed?: boolean;
}

// ConsoleButton is a quiet control in the console's bar: an action, or a
// toggle when pressed is given.
export function ConsoleButton({ label, icon: Icon, iconOnly = false, pressed, className, ...props }: ConsoleButtonProps) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      aria-label={iconOnly ? label : undefined}
      title={iconOnly ? label : undefined}
      className={cn(
        'inline-flex h-7 min-w-7 shrink-0 items-center justify-center gap-1.5 rounded-lg px-[9px] text-[12px] font-semibold transition-colors disabled:opacity-50',
        pressed ? 'bg-console-fill-hover text-console-label' : 'text-console-label-2 hover:bg-console-fill hover:text-console-label',
        iconOnly && 'px-0',
        className,
      )}
      {...props}
    >
      {Icon && <Icon aria-hidden className="h-3.5 w-3.5" strokeWidth={2} />}
      {!iconOnly && label}
    </button>
  );
}

// ConsoleNote fills the console's body with one quiet line, and an action
// when there is something to do, as when the terminal waits for the
// container to run.
export function ConsoleNote({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 p-6 text-center font-sans text-[13px] text-console-label-2">
      <span>{children}</span>
      {action}
    </div>
  );
}
