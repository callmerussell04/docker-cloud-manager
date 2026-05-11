import { Link, useNavigate } from 'react-router-dom';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery } from '@tanstack/react-query';
import { Box, KeyRound } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { useToastStore } from '@/store/toastStore';
import { useAuthStore } from '@/store/authStore';
import { getAuthConfigFn, loginFn } from '@/features/auth/api';
import { type LoginData, createLoginSchema } from '@/features/auth/types';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';
import { API_URL } from '@/config';
import { queryKeys } from '@/shared/api/queryKeys';

export function LoginPage() {
  const navigate = useNavigate();
  const addToast = useToastStore((state) => state.addToast);
  const setAccessToken = useAuthStore((state) => state.setAccessToken);
  const t = useT();
  const { data: authConfig } = useQuery({
    queryKey: queryKeys.auth.config,
    queryFn: getAuthConfigFn,
  });
  const localLoginEnabled = authConfig?.local_login_enabled ?? true;
  const localRegisterEnabled = authConfig?.local_register_enabled ?? true;
  const keycloakEnabled = authConfig?.oidc_providers.some((provider) => provider.name === 'keycloak') ?? false;

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginData>({
    resolver: zodResolver(createLoginSchema(t)),
  });

  const mutation = useMutation({
    mutationFn: loginFn,
    onSuccess: (data) => {
      if (data.access_token) {
        setAccessToken(data.access_token);
        addToast(t('auth.login.success'), 'success');
        navigate('/');
      }
    },
    onError: (error: unknown) => {
      const { message } = getApiErrorMessage(error, t('auth.login.failed'), t);
      addToast(message, 'error');
    },
  });

  const onSubmit = (data: LoginData) => {
    mutation.mutate(data);
  };

  const startKeycloakLogin = () => {
    const redirectAfter = `${window.location.origin}/auth/callback`;
    const params = new URLSearchParams({ redirect_after: redirectAfter });
    window.location.href = `${API_URL}/auth/oidc/keycloak/start?${params.toString()}`;
  };

  return (
    <div className="min-h-screen flex flex-col items-center justify-center p-4">
      <div className="mb-8 flex items-center gap-3 text-indigo-600 dark:text-indigo-400">
        <Box className="w-10 h-10 stroke-[2]" />
        <span className="font-bold text-3xl tracking-tight text-slate-900 dark:text-slate-100">
          Docker Cloud Manager
        </span>
      </div>

      <Card className="w-full max-w-md">
        <CardHeader className="text-center pb-2">
          <CardTitle>{t('auth.login.title')}</CardTitle>
          <p className="text-sm text-slate-500 dark:text-slate-400 mt-2">
            {t('auth.login.subtitle')}
          </p>
        </CardHeader>
        <CardContent>
          {keycloakEnabled && (
            <Button type="button" variant="secondary" className="w-full mt-4" onClick={startKeycloakLogin}>
              <KeyRound className="w-4 h-4 mr-2" />
              {t('auth.login.keycloak')}
            </Button>
          )}

          {localLoginEnabled && (
            <form onSubmit={handleSubmit(onSubmit)} className="space-y-4 mt-4">
              <div className="space-y-2">
                <Label htmlFor="username">{t('form.username')}</Label>
                <Input
                  id="username"
                  type="text"
                  placeholder="username"
                  error={!!errors.username}
                  {...register('username')}
                />
                {errors.username && (
                  <p className="text-sm text-red-500">{errors.username.message}</p>
                )}
              </div>

              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="password">{t('form.password')}</Label>
                </div>
                <Input
                  id="password"
                  type="password"
                  placeholder="••••••••"
                  error={!!errors.password}
                  {...register('password')}
                />
                {errors.password && (
                  <p className="text-sm text-red-500">{errors.password.message}</p>
                )}
              </div>

              <Button
                type="submit"
                className="w-full mt-2"
                isLoading={mutation.isPending}
              >
                {t('auth.login.submit')}
              </Button>
            </form>
          )}

          {localRegisterEnabled && (
            <div className="mt-6 text-center text-sm text-slate-500 dark:text-slate-400">
              {t('auth.login.noAccount')}{' '}
              <Link
                to="/register"
                className="font-medium text-indigo-600 dark:text-indigo-400 hover:underline"
              >
                {t('auth.login.registerLink')}
              </Link>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
