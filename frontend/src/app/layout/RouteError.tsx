import { AlertTriangle, RefreshCcw } from 'lucide-react';
import { Button } from '@/shared/ui/Button';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';

interface RouteErrorProps {
  fullScreen?: boolean;
}

// RouteError replaces a page that failed to render or whose code failed to
// load, as an open tab's does after a new version is deployed. Reloading
// fetches the current code.
export function RouteError({ fullScreen = false }: RouteErrorProps) {
  const t = useT();

  return (
    <div className={cn('flex items-center justify-center p-6', fullScreen ? 'min-h-screen' : 'py-24')} role="alert">
      <div className="max-w-md text-center">
        <div className="mx-auto mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-red-100 text-red-600 dark:bg-red-950 dark:text-red-300">
          <AlertTriangle className="h-8 w-8" />
        </div>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">{t('error.pageTitle')}</h1>
        <p className="mt-2 text-sm text-slate-500 dark:text-slate-400">{t('error.pageMessage')}</p>
        <Button type="button" className="mt-6" onClick={() => window.location.reload()}>
          <RefreshCcw className="mr-2 h-4 w-4" />
          {t('error.reload')}
        </Button>
      </div>
    </div>
  );
}
