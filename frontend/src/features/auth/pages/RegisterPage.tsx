import { Link, useNavigate } from 'react-router-dom';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation } from '@tanstack/react-query';

import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { Button } from '@/shared/ui/Button';
import { PasswordInput } from '@/shared/ui/PasswordInput';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { AuthHeading, AuthNotice, AuthScreen, authButtonClass, authFieldClass } from '@/features/auth/components/AuthScreen';
import { registerFn } from '@/features/auth/api';
import { type RegisterData, createRegisterSchema } from '@/features/auth/schemas';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';

export function RegisterPage() {
  const navigate = useNavigate();
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<RegisterData>({
    resolver: zodResolver(createRegisterSchema(t)),
  });

  const mutation = useMutation({
    mutationFn: registerFn,
    onSuccess: () => {
      notify(t('auth.register.success'), 'success');
      navigate('/login');
    },
  });

  const onSubmit = (data: RegisterData) => {
    mutation.mutate(data);
  };

  return (
    <AuthScreen>
      <form onSubmit={handleSubmit(onSubmit)} className="grid gap-3.5" noValidate>
        <AuthHeading title={t('auth.register.title')} subtitle={t('auth.register.subtitle')} />
        {mutation.isError && (
          <AuthNotice tone="bad">{failureReason(mutation.error, t) ?? t('auth.register.failed')}</AuthNotice>
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

        <Field label={t('form.email')} htmlFor="email" error={errors.email?.message}>
          <Input
            id="email"
            type="email"
            autoComplete="email"
            className={authFieldClass}
            error={!!errors.email}
            {...register('email')}
          />
        </Field>

        <Field label={t('form.password')} htmlFor="password" error={errors.password?.message}>
          <PasswordInput
            id="password"
            autoComplete="new-password"
            className={authFieldClass}
            error={!!errors.password}
            {...register('password')}
          />
        </Field>

        <Button type="submit" className={cn(authButtonClass, 'mt-1')} isLoading={mutation.isPending}>
          {t('auth.register.submit')}
        </Button>

        <p className="mt-1 text-center text-label-2">
          {t('auth.register.hasAccount')}{' '}
          <Link to="/login" className="font-medium text-accent hover:underline">
            {t('auth.register.loginLink')}
          </Link>
        </p>
      </form>
    </AuthScreen>
  );
}
