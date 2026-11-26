import { Link, useNavigate } from 'react-router-dom';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation } from '@tanstack/react-query';
import { KeyRound } from 'lucide-react';

import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { Button } from '@/shared/ui/Button';
import { PasswordInput } from '@/shared/ui/PasswordInput';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { AuthDivider, AuthHeading, AuthNotice, AuthScreen, authButtonClass, authFieldClass } from '@/features/auth/components/AuthScreen';
import { loginFn } from '@/features/auth/api';
import { type LoginData, createLoginSchema } from '@/features/auth/schemas';
import { useAuthConfig } from '@/features/auth/queries';
import { providerAvailability } from '@/features/auth/model';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
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
    resetField,
    setFocus,
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
    // The username stays; the password is cleared for the next try.
    onError: () => {
      resetField('password');
      setFocus('password');
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
    <AuthScreen>
      <div className="grid gap-3.5">
        <AuthHeading title={t('auth.login.title')} subtitle={t('auth.login.subtitle')} />

        {keycloak === 'available' && (
          <Button type="button" variant="secondary" className={cn(authButtonClass, 'gap-2')} onClick={startKeycloakLogin}>
            <KeyRound className="h-4 w-4" />
            {t('auth.login.keycloak')}
          </Button>
        )}
        {keycloak === 'unavailable' && <AuthNotice>{t('auth.login.keycloakUnavailable')}</AuthNotice>}

        {localLoginEnabled && (
          <form onSubmit={handleSubmit(onSubmit)} className="grid gap-3.5" noValidate>
            {keycloak !== 'off' && <AuthDivider>{t('auth.login.or')}</AuthDivider>}
            {mutation.isError && (
              <AuthNotice tone="bad">{failureReason(mutation.error, t) ?? t('auth.login.failed')}</AuthNotice>
            )}
            <Field label={t('form.username')} htmlFor="username" error={errors.username?.message}>
              <Input
                id="username"
                type="text"
                autoComplete="username"
                className={authFieldClass}
                error={!!errors.username}
                {...register('username')}
              />
            </Field>

            <Field label={t('form.password')} htmlFor="password" error={errors.password?.message}>
              <PasswordInput
                id="password"
                autoComplete="current-password"
                className={authFieldClass}
                error={!!errors.password}
                {...register('password')}
              />
            </Field>

            <Button type="submit" className={cn(authButtonClass, 'mt-1')} isLoading={mutation.isPending}>
              {t('auth.login.submit')}
            </Button>
          </form>
        )}

        {localRegisterEnabled && (
          <p className="mt-1 text-center text-label-2">
            {t('auth.login.noAccount')}{' '}
            <Link to="/register" className="font-medium text-accent hover:underline">
              {t('auth.login.registerLink')}
            </Link>
          </p>
        )}
      </div>
    </AuthScreen>
  );
}
