import { useEffect, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { Loader2 } from 'lucide-react';

import { refreshSession } from '@/shared/api/client';
import { useT, type TranslationKey } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Button } from '@/shared/ui/Button';
import { AuthScreen, AuthState, authButtonClass } from '../components/AuthScreen';
import { callbackResult } from '../model';

export function AuthCallbackPage() {
  const navigate = useNavigate();
  const { search } = useLocation();
  const t = useT();
  const result = callbackResult(search);
  // Only a successful sign-in has a session to refresh; the effect depends on
  // this flag rather than on the result object, which is new every render.
  const succeeded = result.ok;
  const [refreshFailed, setRefreshFailed] = useState(false);
  // Why signing in failed, for the card to say with a way back, or null while
  // the refresh is still in flight.
  let failure: TranslationKey | null = null;
  if (!result.ok) failure = result.message;
  else if (refreshFailed) failure = 'auth.callback.reason.unknown';

  useEffect(() => {
    if (!succeeded) return;
    let canceled = false;

    async function completeLogin() {
      try {
        const token = await refreshSession();
        if (!token) {
          throw new Error('missing access token');
        }
        if (!canceled) {
          navigate('/', { replace: true });
        }
      } catch {
        if (!canceled) {
          setRefreshFailed(true);
        }
      }
    }

    void completeLogin();

    return () => {
      canceled = true;
    };
  }, [navigate, succeeded]);

  return (
    <AuthScreen>
      {failure ? (
        <AuthState role="alert" title={t('auth.callback.failed')} description={t(failure)}>
          <Button className={cn(authButtonClass, 'mt-1')} onClick={() => navigate('/login', { replace: true })}>
            {t('auth.callback.back')}
          </Button>
        </AuthState>
      ) : (
        <AuthState
          role="status"
          mark={<Loader2 aria-hidden className="h-7 w-7 animate-spin text-accent-fill" />}
          title={t('auth.callback.loading')}
          description={t('auth.callback.returning')}
        />
      )}
    </AuthScreen>
  );
}
