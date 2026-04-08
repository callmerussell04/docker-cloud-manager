import { type HTMLAttributes, forwardRef } from 'react';
import { cn } from '@/lib/utils';

interface BadgeProps extends HTMLAttributes<HTMLDivElement> {
  variant?: 'default' | 'success' | 'warning' | 'error' | 'info';
}

export const Badge = forwardRef<HTMLDivElement, BadgeProps>(
  ({ className, variant = 'default', ...props }, ref) => {
    const variants = {
      default: "bg-slate-100/50 text-slate-800 dark:bg-slate-800/50 dark:text-slate-300 border-slate-200 dark:border-slate-700",
      success: "bg-green-100/50 text-green-800 dark:bg-green-900/30 dark:text-green-300 border-green-200 dark:border-green-800",
      warning: "bg-yellow-100/50 text-yellow-800 dark:bg-yellow-900/30 dark:text-yellow-300 border-yellow-200 dark:border-yellow-800",
      error: "bg-red-100/50 text-red-800 dark:bg-red-900/30 dark:text-red-300 border-red-200 dark:border-red-800",
      info: "bg-blue-100/50 text-blue-800 dark:bg-blue-900/30 dark:text-blue-300 border-blue-200 dark:border-blue-800",
    };

    return (
      <div
        ref={ref}
        className={cn(
          "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors backdrop-blur-md",
          variants[variant],
          className
        )}
        {...props}
      />
    );
  }
);
Badge.displayName = 'Badge';