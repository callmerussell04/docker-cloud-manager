import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { refreshSession, retrySessionRefresh } from '@/shared/api/client';
import { leaveForProviderSignOut, logoutFn } from '@/features/auth/api';
import { useAuthStore } from '@/shared/api/sessionStore';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Button } from '@/shared/ui/Button';
import { AuthScreen, AuthState, authButtonClass } from '@/features/auth/components/AuthScreen';

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
        if (sessionIssue === 'logout') leaveForProviderSignOut(await logoutFn());
        else await retrySessionRefresh();
      } catch {
        // The coordinator keeps the recovery screen and preserves the cookie.
      } finally {
        setRetrying(false);
      }
    };
    return (
      <AuthScreen>
        <AuthState role="alert" title={t('auth.session.unavailable')} description={t('auth.session.uncertain')}>
          <Button className={cn(authButtonClass, 'mt-1')} isLoading={retrying} onClick={() => void retry()}>
            {t(sessionIssue === 'logout' ? 'auth.session.retryLogout' : 'auth.session.retry')}
          </Button>
          <a className="font-medium text-accent hover:underline" href="/login">{t('auth.session.signIn')}</a>
        </AuthState>
      </AuthScreen>
    );
  }
  return <>{children}</>;
}
