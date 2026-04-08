import { type SelectHTMLAttributes, forwardRef } from 'react';
import { cn } from '@/lib/utils';

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  error?: boolean;
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(
  ({ className, error, ...props }, ref) => {
    return (
      <select
        ref={ref}
        className={cn(
          "flex w-full rounded-xl bg-white/50 dark:bg-slate-900/50 backdrop-blur-md border border-white/40 dark:border-slate-700/50 px-4 py-2.5 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500 disabled:cursor-not-allowed disabled:opacity-50 transition-all appearance-none",
          error && "border-red-500 focus-visible:ring-red-500 dark:border-red-500/50",
          className
        )}
        {...props}
      />
    );
  }
);
Select.displayName = 'Select';