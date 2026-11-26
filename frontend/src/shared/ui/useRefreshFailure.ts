import { useEffect } from 'react';
import { failureReason } from '@/shared/api/apiError';
import { dateLocale, useLocale, useTRef } from '@/shared/i18n';
import { useNotificationStore } from './notificationStore';

// The parts of a list's query a failed refresh is read from.
interface RefreshedQuery {
  isRefetchError: boolean;
  error: unknown;
  errorUpdatedAt: number;
  dataUpdatedAt: number;
}

// useRefreshFailure watches a list's query: when a refresh fails while the
// list still shows what it loaded before, it says so once in a notification
// (what failed, and since when the rows are from) and answers that the rows
// are stale, so the list dims them. A list that never loaded shows its
// ErrorState instead.
export function useRefreshFailure(query: RefreshedQuery, title: string): boolean {
  const notify = useNotificationStore((store) => store.notify);
  const tRef = useTRef();
  const locale = useLocale();
  const { isRefetchError, error, errorUpdatedAt, dataUpdatedAt } = query;

  useEffect(() => {
    if (!isRefetchError) return;
    const t = tRef.current;
    const time = new Date(dataUpdatedAt).toLocaleTimeString(dateLocale(locale), { hour: '2-digit', minute: '2-digit' });
    const reason = [t('common.refreshFailedSince', { time }), failureReason(error, t)].filter(Boolean).join(' ');
    notify(title, 'error', reason);
    // errorUpdatedAt names each failure, so a later one notifies again.
  }, [isRefetchError, errorUpdatedAt, dataUpdatedAt, error, locale, notify, title, tRef]);

  return isRefetchError;
}
