import { useEffect, useState } from 'react';

import { useT, type TFunction } from '@/shared/i18n';

function formatRemaining(t: TFunction, totalSeconds: number) {
  if (totalSeconds <= 0) return null;
  if (totalSeconds < 60) return t('containers.remaining.underMinute');

  const days = Math.floor(totalSeconds / 86400);
  const hours = Math.floor((totalSeconds % 86400) / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (days > 0) return t('containers.remaining.days', { days, hours });
  if (hours > 0) return t('containers.remaining.hours', { hours, minutes });
  return t('containers.remaining.minutes', { minutes, seconds });
}

// useAutoStopLabel is the countdown to a running container's auto-stop,
// renewed every second, or null while no auto-stop is set.
export function useAutoStopLabel(status: string, ttlDeadline?: number) {
  const t = useT();
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000));
  const counting = status === 'running' && !!ttlDeadline && ttlDeadline > 0;

  useEffect(() => {
    if (!counting) return;
    const timer = window.setInterval(() => {
      setNow(Math.floor(Date.now() / 1000));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [counting]);

  if (!counting) return null;
  const remaining = formatRemaining(t, Math.max(0, ttlDeadline - now));
  return remaining ? t('containers.autoStopIn', { time: remaining }) : t('containers.autoStopSoon');
}
