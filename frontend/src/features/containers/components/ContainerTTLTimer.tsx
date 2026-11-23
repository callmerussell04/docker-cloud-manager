import { Clock3 } from 'lucide-react';

import { useAutoStopLabel } from './useAutoStopLabel';
import { cn } from '@/shared/lib/utils';

interface ContainerTTLTimerProps {
  status: string;
  ttlDeadline?: number;
  className?: string;
}

export function ContainerTTLTimer({ status, ttlDeadline, className }: ContainerTTLTimerProps) {
  const label = useAutoStopLabel(status, ttlDeadline);
  if (!label) return null;

  return (
    <div
      className={cn('mt-1 flex min-w-0 items-center gap-1 text-xs text-amber-600 dark:text-amber-400', className)}
      title={label}
    >
      <Clock3 className="h-3 w-3 shrink-0" />
      <span className="truncate">{label}</span>
    </div>
  );
}
