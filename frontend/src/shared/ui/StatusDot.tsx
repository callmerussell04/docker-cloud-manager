import type { ReactNode } from 'react';
import { cn } from '@/shared/lib/utils';

// StatusTone is what a state means to the user: working, failing, changing,
// or at rest.
export type StatusTone = 'ok' | 'bad' | 'busy' | 'idle';

const dotClass: Record<StatusTone, string> = {
  ok: 'bg-green',
  bad: 'bg-red',
  busy: 'bg-orange animate-[status-pulse_1.4s_ease-in-out_infinite]',
  idle: 'shadow-[inset_0_0_0_1.5px_var(--label-3)]',
};

// Dot is the coloured mark alone, for a place where the label sits elsewhere
// on the same line.
export function Dot({ tone, className }: { tone: StatusTone; className?: string }) {
  return <span aria-hidden className={cn('inline-block h-2 w-2 shrink-0 rounded-full', dotClass[tone], className)} />;
}

// StatusDot shows a state as a dot beside its label: a status colour never
// stands alone.
export function StatusDot({ tone, children, className }: { tone: StatusTone; children: ReactNode; className?: string }) {
  return (
    <span className={cn('inline-flex min-w-0 items-center gap-1.5', className)}>
      <Dot tone={tone} />
      <span className="truncate">{children}</span>
    </span>
  );
}
