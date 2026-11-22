import { RefreshCcw } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { StateMessage } from './StateMessage';

interface ErrorStateProps {
  title: string;
  // message is the reason, from the API error mapping, never raw server text.
  message?: string;
  onRetry?: () => void;
  isRetrying?: boolean;
}

// ErrorState is something that never loaded: it says so in red, why, and
// offers Try again.
export function ErrorState({ title, message, onRetry, isRetrying }: ErrorStateProps) {
  const t = useT();
  return (
    <StateMessage
      role="alert"
      tone="bad"
      title={title}
      description={message}
      action={onRetry ? { label: t('common.tryAgain'), onClick: onRetry, icon: RefreshCcw, busy: isRetrying } : undefined}
    />
  );
}
