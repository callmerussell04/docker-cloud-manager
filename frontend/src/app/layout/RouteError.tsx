import { RefreshCcw, TriangleAlert } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { FrostWindow } from '@/shared/ui/FrostWindow';
import { ListMessage } from '@/shared/ui/ListMessage';

interface RouteErrorProps {
  fullScreen?: boolean;
}

// RouteError replaces a page that failed to render or whose code failed to
// load, as an open tab's does after a new version is deployed, as a page that
// could not be loaded: a warning, what failed, why, and Reload, which fetches
// the current code.
// In place it sits on the layout's frost; full screen it brings its own.
export function RouteError({ fullScreen = false }: RouteErrorProps) {
  const t = useT();

  return (
    <div className={cn('flex items-center justify-center p-6', fullScreen ? 'relative isolate min-h-screen' : 'py-24')}>
      {fullScreen && <FrostWindow className="absolute inset-0 -z-10" />}
      <ListMessage
        role="alert"
        icon={<TriangleAlert aria-hidden className="mb-1 h-7 w-7 text-red-text" strokeWidth={1.8} />}
        title={t('error.pageTitle')}
        description={t('error.pageMessage')}
        action={{ label: t('error.reload'), onClick: () => window.location.reload(), icon: RefreshCcw }}
      />
    </div>
  );
}
