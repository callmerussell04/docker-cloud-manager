import { type HTMLAttributes, forwardRef } from 'react';
import { cn } from '@/shared/lib/utils';

export type BadgeVariant = 'default' | 'success' | 'warning' | 'error' | 'info';

interface BadgeProps extends HTMLAttributes<HTMLDivElement> {
  variant?: BadgeVariant;
}

// A status colour is never shown alone: it is a dot beside the label.
const dotClass: Record<BadgeVariant, string | undefined> = {
  default: undefined,
  success: 'bg-green',
  warning: 'bg-orange',
  error: 'bg-red',
  info: 'bg-label-3',
};

export const Badge = forwardRef<HTMLDivElement, BadgeProps>(
  ({ className, variant = 'default', children, ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full bg-fill px-2.5 py-0.5 text-xs font-semibold text-label-2 transition-colors',
        className
      )}
      {...props}
    >
      {dotClass[variant] && <span aria-hidden className={cn('h-1.5 w-1.5 shrink-0 rounded-full', dotClass[variant])} />}
      {children}
    </div>
  )
);
Badge.displayName = 'Badge';
