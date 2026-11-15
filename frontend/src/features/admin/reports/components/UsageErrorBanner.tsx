import { AlertTriangle, RefreshCcw } from 'lucide-react';

import { Button } from '@/shared/ui/Button';
import { useT } from '@/shared/i18n';

export function UsageErrorBanner({
  message,
  onRetry,
  isRetrying,
}: {
  message: string;
  onRetry: () => void;
  isRetrying: boolean;
}) {
  const t = useT();

  return (
    <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-start gap-2">
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{message}</span>
        </div>
        <Button type="button" variant="secondary" onClick={onRetry} isLoading={isRetrying} className="h-9 px-3">
          <RefreshCcw className="mr-2 h-4 w-4" />
          {t('common.refresh')}
        </Button>
      </div>
    </div>
  );
}
