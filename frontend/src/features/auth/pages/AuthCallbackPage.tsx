import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';

import { refreshSession } from '@/shared/api/client';
import { useToastStore } from '@/shared/ui/toastStore';
import { useT } from '@/shared/i18n';

export function AuthCallbackPage() {
  const navigate = useNavigate();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  useEffect(() => {
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
          addToast(t('auth.callback.failed'), 'error');
          navigate('/login', { replace: true });
        }
      }
    }

    void completeLogin();

    return () => {
      canceled = true;
    };
  }, [addToast, navigate, t]);

  return (
    <div className="min-h-screen flex items-center justify-center p-4 text-sm text-slate-500 dark:text-slate-400">
      {t('auth.callback.loading')}
    </div>
  );
}
