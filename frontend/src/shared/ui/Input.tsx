import { type InputHTMLAttributes, forwardRef } from 'react';
import { cn } from '@/shared/lib/utils';

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  error?: boolean;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ className, error, ...props }, ref) => {
    return (
      <input
        ref={ref}
        className={cn(
          "flex w-full rounded-xl bg-fill text-label border border-transparent px-4 py-2.5 text-sm file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-label-3 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-fill disabled:cursor-not-allowed disabled:opacity-50 transition-all",
          error && "border-red focus-visible:ring-red",
          className
        )}
        {...props}
      />
    );
  }
);
Input.displayName = 'Input';