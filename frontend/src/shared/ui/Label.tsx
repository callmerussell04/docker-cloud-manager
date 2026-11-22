import { type LabelHTMLAttributes, forwardRef } from 'react';
import { cn } from '@/shared/lib/utils';

export const Label = forwardRef<HTMLLabelElement, LabelHTMLAttributes<HTMLLabelElement>>(
  ({ className, ...props }, ref) => (
    <label
      ref={ref}
      className={cn(
        "text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 text-label-2",
        className
      )}
      {...props}
    />
  )
);
Label.displayName = 'Label';