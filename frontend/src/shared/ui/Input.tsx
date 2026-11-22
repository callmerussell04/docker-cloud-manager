import { type InputHTMLAttributes, type ReactNode, forwardRef } from 'react';
import { cn } from '@/shared/lib/utils';

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  error?: boolean;
  // suffix follows the field on its line, such as the base domain after a
  // subdomain.
  suffix?: ReactNode;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ className, error, suffix, ...props }, ref) => {
    const input = (
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
    if (!suffix) return input;
    return (
      <div className="flex items-center gap-2">
        {input}
        <span className="whitespace-nowrap text-label-2">{suffix}</span>
      </div>
    );
  }
);
Input.displayName = 'Input';
