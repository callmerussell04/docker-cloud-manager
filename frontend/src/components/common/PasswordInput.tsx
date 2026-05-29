import { forwardRef, useState, type InputHTMLAttributes } from 'react';
import { Eye, EyeOff } from 'lucide-react';

import { Input } from '@/components/ui/Input';
import { useT } from '@/lib/i18n';
import { cn } from '@/lib/utils';

interface PasswordInputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  error?: boolean;
}

export const PasswordInput = forwardRef<HTMLInputElement, PasswordInputProps>(
  ({ className, disabled, error, ...props }, ref) => {
    const [isVisible, setIsVisible] = useState(false);
    const t = useT();
    const label = isVisible ? t('form.passwordHide') : t('form.passwordShow');

    return (
      <div className="relative">
        <Input
          ref={ref}
          type={isVisible ? 'text' : 'password'}
          disabled={disabled}
          error={error}
          className={cn('pr-12', className)}
          {...props}
        />
        <button
          type="button"
          onClick={() => setIsVisible((value) => !value)}
          onMouseDown={(event) => event.preventDefault()}
          disabled={disabled}
          className="absolute right-2 top-1/2 inline-flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-lg text-slate-500 transition-colors hover:bg-slate-900/5 hover:text-slate-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 disabled:pointer-events-none disabled:opacity-50 dark:text-slate-400 dark:hover:bg-white/10 dark:hover:text-slate-200"
          aria-label={label}
          title={label}
        >
          {isVisible ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
        </button>
      </div>
    );
  }
);
PasswordInput.displayName = 'PasswordInput';
