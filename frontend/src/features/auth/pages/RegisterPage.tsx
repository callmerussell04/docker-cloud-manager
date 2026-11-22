import { Link, useNavigate } from 'react-router-dom';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation } from '@tanstack/react-query';
import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/Card';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { Button } from '@/shared/ui/Button';
import { AuthHeader } from '@/features/auth/components/AuthHeader';
import { PasswordInput } from '@/shared/ui/PasswordInput';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { registerFn } from '@/features/auth/api';
import { type RegisterData, createRegisterSchema } from '@/features/auth/schemas';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';

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
    onError: (error: unknown) => {
      notify(t('auth.register.failed'), 'error', failureReason(error, t));
    },
  });

  const onSubmit = (data: RegisterData) => {
    mutation.mutate(data);
  };

  return (
    <div className="relative isolate min-h-screen flex flex-col">
      <div aria-hidden className="frost-veil fixed inset-0 -z-10" />
      <AuthHeader />

      <main className="flex flex-1 flex-col items-center justify-center px-4 py-10">
        <Card className="w-full max-w-md">
          <CardHeader className="text-center pb-2">
            <CardTitle>{t('auth.register.title')}</CardTitle>
            <p className="text-sm text-slate-500 dark:text-slate-400 mt-2">
              {t('auth.register.subtitle')}
            </p>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit(onSubmit)} className="space-y-4 mt-4">
              <Field label={t('form.username')} htmlFor="username" error={errors.username?.message as string | undefined}>
                <Input
                  id="username"
                  type="text"
                  placeholder="johndoe"
                  error={!!errors.username}
                  {...register('username')}
                />
              </Field>

              <Field label={t('form.email')} htmlFor="email" error={errors.email?.message as string | undefined}>
                <Input
                  id="email"
                  type="email"
                  placeholder="john@example.com"
                  error={!!errors.email}
                  {...register('email')}
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
                {t('auth.register.submit')}
              </Button>
            </form>

            <div className="mt-6 text-center text-sm text-slate-500 dark:text-slate-400">
              {t('auth.register.hasAccount')}{' '}
              <Link
                to="/login"
                className="font-medium text-accent hover:underline"
              >
                {t('auth.register.loginLink')}
              </Link>
            </div>
          </CardContent>
        </Card>
      </main>
    </div>
  );
}
