import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { refreshSession, retrySessionRefresh, logoutSession } from '@/api/axios';
import { useAuthStore } from '@/store/authStore';
import { useT } from '@/lib/i18n';

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const { setInitialized, sessionIssue } = useAuthStore();
  const [retrying, setRetrying] = useState(false);
  const queries = useQueryClient();
  const t = useT();

  useEffect(() => useAuthStore.subscribe((state, previous) => {
    if (!state.accessToken && previous.accessToken) {
      void queries.cancelQueries();
      queries.clear();
    }
  }), [queries]);

  useEffect(() => {
    // Explicit sign-in must remain reachable after an uncertain refresh result.
    if (['/login', '/register'].includes(window.location.pathname)) {
      setInitialized(true);
      return;
    }
    void refreshSession().catch(() => {}).finally(() => setInitialized(true));
  }, [setInitialized]);

  if (sessionIssue) {
    const retry = async () => {
      setRetrying(true);
      try {
        if (sessionIssue === 'logout') await logoutSession();
        else await retrySessionRefresh();
      } catch {
        // The coordinator keeps the recovery screen and preserves the cookie.
      } finally {
        setRetrying(false);
      }
    };
    return (
      <main className="flex min-h-screen items-center justify-center bg-slate-50 p-6 dark:bg-slate-950">
        <section className="max-w-md space-y-4 rounded-xl bg-white p-6 text-slate-900 shadow dark:bg-slate-900 dark:text-slate-100" role="alert">
          <h1 className="text-lg font-semibold">{t('auth.session.unavailable')}</h1>
          <p>{t('auth.session.uncertain')}</p>
          <div className="flex gap-4">
            <button className="rounded bg-indigo-600 px-4 py-2 text-white" disabled={retrying} onClick={() => void retry()}>
              {t(sessionIssue === 'logout' ? 'auth.session.retryLogout' : 'auth.session.retry')}
            </button>
            <a className="px-4 py-2 underline" href="/login">{t('auth.session.signIn')}</a>
          </div>
        </section>
      </main>
    );
  }
  return <>{children}</>;
}
