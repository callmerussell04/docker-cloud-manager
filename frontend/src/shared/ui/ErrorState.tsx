import { CloudOff, RefreshCcw, TriangleAlert } from 'lucide-react';
import { failureReason, isConnectionError } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { GhostRows, type GhostShape } from './GhostRows';
import { ListMessage } from './ListMessage';
import { StateMessage } from './StateMessage';

interface ErrorStateProps extends GhostShape {
  title: string;
  // error is what failed: it gives the reason, and whether the server
  // answered at all.
  error?: unknown;
  // message is the reason when there is no error to map, already safe to
  // show; never raw server text.
  message?: string;
  onRetry?: () => void;
  isRetrying?: boolean;
  // compact is the small form for a panel within a page, left-aligned with
  // a red title; otherwise the failure takes the list's or the page's place.
  compact?: boolean;
}

// ErrorState is something that never loaded. In a list (a shape given) it
// is the list's rows greyed out with the failure over them; on a page it is
// the failure centred; in a panel (compact) a red title and line. It says
// what failed, why (the server did not answer, or its mapped reason), and
// offers Try again.
export function ErrorState({ title, error, message, onRetry, isRetrying, compact = false, ...shape }: ErrorStateProps) {
  const t = useT();
  const offline = isConnectionError(error);
  const reason = offline ? t('common.loadFailedOffline') : (message ?? failureReason(error, t) ?? t('common.loadFailedReason'));
  const action = onRetry ? { label: t('common.tryAgain'), onClick: onRetry, icon: RefreshCcw, busy: isRetrying } : undefined;

  if (compact) return <StateMessage role="alert" tone="bad" title={title} description={reason} action={action} />;

  const Icon = offline ? CloudOff : TriangleAlert;
  const failure = (
    <ListMessage
      role="alert"
      icon={<Icon aria-hidden className="mb-1 h-7 w-7 text-red-text" strokeWidth={1.8} />}
      title={title}
      description={reason}
      action={action}
    />
  );
  if (shape.template || shape.columns) return <GhostRows tone="failed" {...shape}>{failure}</GhostRows>;
  return <div className="grid min-h-[50vh] place-items-center px-4">{failure}</div>;
}
