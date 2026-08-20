import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';

import { publicApi } from '@/api/axios';
import { useAuthStore } from '@/store/authStore';
import { useToastStore } from '@/store/toastStore';
import { useT } from '@/lib/i18n';

export function AuthCallbackPage() {
  const navigate = useNavigate();
  const setAccessToken = useAuthStore((state) => state.setAccessToken);
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  useEffect(() => {
    let canceled = false;

    async function completeLogin() {
      try {
        const response = await publicApi.post<{ access_token?: string }>('/auth/refresh');
        const token = response.data?.access_token;
        if (!token) {
          throw new Error('missing access token');
        }
        if (!canceled) {
          setAccessToken(token);
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
  }, [addToast, navigate, setAccessToken, t]);

  return (
    <div className="min-h-screen flex items-center justify-center p-4 text-sm text-slate-500 dark:text-slate-400">
      {t('auth.callback.loading')}
    </div>
  );
}
