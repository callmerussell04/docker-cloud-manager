import { useEffect, useState } from 'react';
import { Clock3 } from 'lucide-react';

import { useT } from '@/lib/i18n';
import { cn } from '@/lib/utils';

interface ContainerTTLTimerProps {
  status: string;
  ttlDeadline?: number;
  className?: string;
}

function formatRemaining(totalSeconds: number) {
  if (totalSeconds <= 0) return null;
  if (totalSeconds < 60) return '<1m';

  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m ${seconds}s`;
}

export function ContainerTTLTimer({ status, ttlDeadline, className }: ContainerTTLTimerProps) {
  const t = useT();
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000));

  useEffect(() => {
    if (status !== 'running' || !ttlDeadline) return;
    const timer = window.setInterval(() => {
      setNow(Math.floor(Date.now() / 1000));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [status, ttlDeadline]);

  if (status !== 'running' || !ttlDeadline || ttlDeadline <= 0) {
    return null;
  }

  const remaining = formatRemaining(Math.max(0, ttlDeadline - now));
  const label = remaining ? t('containers.autoStopIn', { time: remaining }) : t('containers.autoStopSoon');

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
