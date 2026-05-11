import { AlertTriangle, RefreshCcw } from 'lucide-react';
import { Button } from '@/components/ui/Button';
import { useT } from '@/lib/i18n';

interface ErrorStateProps {
  title?: string;
  message: string;
  onRetry?: () => void;
  isRetrying?: boolean;
}

export function ErrorState({ title, message, onRetry, isRetrying }: ErrorStateProps) {
  const t = useT();

  return (
    <div className="rounded-xl border border-red-200 bg-red-50 p-6 text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
      <div className="flex items-start gap-3">
        <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0" />
        <div className="min-w-0">
          {title && <h2 className="text-lg font-semibold">{title}</h2>}
          <p className="mt-1 text-sm">{message}</p>
          {onRetry && (
            <Button type="button" variant="secondary" onClick={onRetry} isLoading={isRetrying} className="mt-4">
              <RefreshCcw className="mr-2 h-4 w-4" />
              {t('common.refresh')}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
