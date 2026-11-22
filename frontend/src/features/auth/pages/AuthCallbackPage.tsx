import { useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';

import { refreshSession } from '@/shared/api/client';
import { useToastStore } from '@/shared/ui/toastStore';
import { useT } from '@/shared/i18n';
import { callbackResult } from '../model';

export function AuthCallbackPage() {
  const navigate = useNavigate();
  const { search } = useLocation();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  useEffect(() => {
    let canceled = false;
    const result = callbackResult(search);

    // A failed sign-in has no session to refresh: show why and go back to the login page.
    if (!result.ok) {
      addToast(t(result.message), 'error');
      navigate('/login', { replace: true });
      return;
    }

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
          addToast(t('auth.callback.failed'), 'error');
          navigate('/login', { replace: true });
        }
      }
    }

    void completeLogin();

    return () => {
      canceled = true;
    };
  }, [addToast, navigate, search, t]);

  return (
    <div className="relative isolate min-h-screen flex items-center justify-center p-4 text-sm text-label-2">
      <div aria-hidden className="frost-veil fixed inset-0 -z-10" />
      {t('auth.callback.loading')}
    </div>
  );
}
