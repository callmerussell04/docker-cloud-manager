import { Link, useNavigate } from 'react-router-dom';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation } from '@tanstack/react-query';
import { Box } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { useToastStore } from '@/store/toastStore';
import { registerFn } from '@/features/auth/api';
import { type RegisterData, createRegisterSchema } from '@/features/auth/types';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';

export function RegisterPage() {
  const navigate = useNavigate();
  const addToast = useToastStore((state) => state.addToast);
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
      addToast(t('auth.register.success'), 'success');
      navigate('/login');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, t('auth.register.failed'), t);
      addToast(message, 'error', { requestId });
    },
  });

  const onSubmit = (data: RegisterData) => {
    mutation.mutate(data);
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
          <CardTitle>{t('auth.register.title')}</CardTitle>
          <p className="text-sm text-slate-500 dark:text-slate-400 mt-2">
            {t('auth.register.subtitle')}
          </p>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4 mt-4">
            <div className="space-y-2">
              <Label htmlFor="username">{t('form.username')}</Label>
              <Input
                id="username"
                type="text"
                placeholder="johndoe"
                error={!!errors.username}
                {...register('username')}
              />
              {errors.username && (
                <p className="text-sm text-red-500">{errors.username.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="email">{t('form.email')}</Label>
              <Input
                id="email"
                type="email"
                placeholder="john@example.com"
                error={!!errors.email}
                {...register('email')}
              />
              {errors.email && (
                <p className="text-sm text-red-500">{errors.email.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="password">{t('form.password')}</Label>
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
              {t('auth.register.submit')}
            </Button>
          </form>

          <div className="mt-6 text-center text-sm text-slate-500 dark:text-slate-400">
            {t('auth.register.hasAccount')}{' '}
            <Link
              to="/login"
              className="font-medium text-indigo-600 dark:text-indigo-400 hover:underline"
            >
              {t('auth.register.loginLink')}
            </Link>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
