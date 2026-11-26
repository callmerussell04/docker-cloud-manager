import { forwardRef, useState, type InputHTMLAttributes } from 'react';
import { Eye, EyeOff } from 'lucide-react';

import { Input } from '@/shared/ui/Input';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';

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
          className="absolute right-2 top-1/2 inline-flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-lg text-label-3 transition-colors hover:bg-fill hover:text-label focus:outline-none focus:ring-2 focus:ring-accent-fill disabled:pointer-events-none disabled:opacity-50"
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
