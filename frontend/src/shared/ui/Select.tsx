import { type SelectHTMLAttributes, forwardRef } from 'react';
import { cn } from '@/shared/lib/utils';

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  error?: boolean;
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(
  ({ className, error, ...props }, ref) => {
    return (
      <select
        ref={ref}
        className={cn(
          "flex w-full rounded-xl bg-fill text-label border border-transparent px-4 py-2.5 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-fill disabled:cursor-not-allowed disabled:opacity-50 transition-all appearance-none",
          error && "border-red focus-visible:ring-red",
          className
        )}
        {...props}
      />
    );
  }
);
Select.displayName = 'Select';