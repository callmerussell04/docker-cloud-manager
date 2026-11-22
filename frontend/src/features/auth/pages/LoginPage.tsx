import { Link, useNavigate } from 'react-router-dom';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation } from '@tanstack/react-query';
import { KeyRound } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/Card';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { Button } from '@/shared/ui/Button';
import { AuthHeader } from '@/features/auth/components/AuthHeader';
import { PasswordInput } from '@/shared/ui/PasswordInput';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { loginFn } from '@/features/auth/api';
import { type LoginData, createLoginSchema } from '@/features/auth/schemas';
import { useAuthConfig } from '@/features/auth/queries';
import { providerAvailability } from '@/features/auth/model';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { API_URL } from '@/shared/lib/config';

export function LoginPage() {
  const navigate = useNavigate();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();
  const { data: authConfig } = useAuthConfig();
  const localLoginEnabled = authConfig?.local_login_enabled ?? true;
  const localRegisterEnabled = authConfig?.local_register_enabled ?? true;
  const keycloak = providerAvailability(authConfig, 'keycloak');

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<LoginData>({
    resolver: zodResolver(createLoginSchema(t)),
  });

  const mutation = useMutation({
    mutationFn: loginFn,
    // loginFn has already handed the token to the session.
    onSuccess: () => {
      notify(t('auth.login.success'), 'success');
      navigate('/');
    },
    onError: (error: unknown) => {
      notify(t('auth.login.failed'), 'error', failureReason(error, t));
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
    <div className="relative isolate min-h-screen flex flex-col">
      <div aria-hidden className="frost-veil fixed inset-0 -z-10" />
      <AuthHeader />

      <main className="flex flex-1 flex-col items-center justify-center px-4 py-10">
        <Card className="w-full max-w-md">
          <CardHeader className="text-center pb-2">
            <CardTitle>{t('auth.login.title')}</CardTitle>
            <p className="text-sm text-slate-500 dark:text-slate-400 mt-2">
              {t('auth.login.subtitle')}
            </p>
          </CardHeader>
          <CardContent>
            {keycloak !== 'off' && (
              <>
                <Button
                  type="button"
                  variant="secondary"
                  className="w-full mt-4"
                  onClick={startKeycloakLogin}
                  disabled={keycloak === 'unavailable'}
                  aria-describedby={keycloak === 'unavailable' ? 'keycloak-unavailable' : undefined}
                >
                  <KeyRound className="w-4 h-4 mr-2" />
                  {t('auth.login.keycloak')}
                </Button>
                {keycloak === 'unavailable' && (
                  <p id="keycloak-unavailable" className="mt-2 text-sm text-slate-500 dark:text-slate-400">
                    {t('auth.login.keycloakUnavailable')}
                  </p>
                )}
              </>
            )}

            {localLoginEnabled && (
              <form onSubmit={handleSubmit(onSubmit)} className="space-y-4 mt-4">
                <Field label={t('form.username')} htmlFor="username" error={errors.username?.message as string | undefined}>
                  <Input
                    id="username"
                    type="text"
                    placeholder="username"
                    error={!!errors.username}
                    {...register('username')}
                  />
                </Field>

                <Field label={t('form.password')} htmlFor="password" error={errors.password?.message as string | undefined}>
                  <PasswordInput
                    id="password"
                    placeholder="••••••••"
                    error={!!errors.password}
                    {...register('password')}
                  />
                </Field>

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
                  className="font-medium text-accent hover:underline"
                >
                  {t('auth.login.registerLink')}
                </Link>
              </div>
            )}
          </CardContent>
        </Card>
      </main>
    </div>
  );
}
