import { cloneElement, useId, type ReactElement, type ReactNode } from 'react';
import { cn } from '@/shared/lib/utils';
import { Label } from './Label';

interface FieldProps {
  label?: ReactNode;
  // htmlFor is the control's id, so the label names it.
  htmlFor?: string;
  hint?: ReactNode;
  // error says what is wrong and how to fix it; it replaces nothing, the
  // hint stays above it.
  error?: string;
  className?: string;
  // children is the one control the label, hint and error describe.
  children: ReactElement<{ 'aria-describedby'?: string; 'aria-invalid'?: boolean }>;
}

// Field is a form control with its label above it and its hint or error
// below it, the error in red. The control is described by both and marked
// invalid while the error shows.
export function Field({ label, htmlFor, hint, error, className, children }: FieldProps) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  const describedBy = [children.props['aria-describedby'], hintId, errorId].filter(Boolean).join(' ') || undefined;
  const control = cloneElement(children, { 'aria-describedby': describedBy, ...(error ? { 'aria-invalid': true } : {}) });

  return (
    <div className={cn('grid grid-cols-[minmax(0,1fr)] gap-1.5', className)}>
      {label && <Label htmlFor={htmlFor}>{label}</Label>}
      {control}
      {hint && <p id={hintId} className="text-xs leading-snug text-label-2">{hint}</p>}
      {error && <p id={errorId} className="text-xs leading-snug text-red-text">{error}</p>}
    </div>
  );
}
