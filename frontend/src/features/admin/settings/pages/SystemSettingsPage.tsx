import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { RefreshCcw, Save, Settings } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/Card';
import { Button } from '@/shared/ui/Button';
import { getApiErrorMessage } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { useAuthConfig } from '@/features/auth/queries';
import type { SystemConfig } from '../model';
import { type SystemConfigForm, systemConfigSchema } from '../schemas';
import { useSystemConfig, useUpdateSystemConfig } from '../queries';
import { sections } from '../components/configFields';
import { ConfigField } from '../components/ConfigField';
import { AuthSettingsStatus } from '../components/AuthSettingsStatus';

export function SystemSettingsPage() {
  const t = useT();

  const { data: config, isLoading, isError, error, isFetching, refetch } = useSystemConfig();
  const authConfigQuery = useAuthConfig();

  const { register, handleSubmit, reset, setValue, watch, formState: { errors } } = useForm<SystemConfigForm>({
    resolver: zodResolver(systemConfigSchema),
  });

  useEffect(() => {
    if (config) {
      reset(config);
    }
  }, [config, reset]);

  const mutation = useUpdateSystemConfig();

  const onSubmit = (data: SystemConfigForm) => {
    mutation.mutate(data as SystemConfig);
  };

  if (isLoading) {
    return <div className="h-96 rounded-2xl bg-white/40 animate-pulse dark:bg-slate-900/40" />;
  }

  if (isError) {
    const { message } = getApiErrorMessage(error, t('admin.settings.loadFailed'), t);

    return (
      <div className="rounded-xl border border-red-200 bg-red-50 p-6 text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
        <h1 className="text-xl font-semibold">{t('admin.settings.loadFailed')}</h1>
        <p className="mt-2 text-sm">{message}</p>
        <Button type="button" variant="secondary" onClick={() => refetch()} isLoading={isFetching} className="mt-4">
          <RefreshCcw className="mr-2 h-4 w-4" />
          {t('common.refresh')}
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">{t('admin.settings.title')}</h1>
        <p className="mt-1 text-slate-500 dark:text-slate-400">{t('admin.settings.subtitle')}</p>
      </div>

      <AuthSettingsStatus
        authConfig={authConfigQuery.data}
        isLoading={authConfigQuery.isLoading}
        isError={authConfigQuery.isError}
        t={t}
      />

      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        {sections.map((section) => (
          <Card key={section.titleKey}>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-lg">
                <Settings className="h-5 w-5 text-indigo-600 dark:text-indigo-400" />
                {t(section.titleKey)}
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">
                {section.fields.map((field) => (
                  <ConfigField
                    key={field.name}
                    field={field}
                    register={register}
                    setValue={setValue}
                    watch={watch}
                    error={errors[field.name as keyof SystemConfigForm]}
                    t={t}
                  />
                ))}
              </div>
            </CardContent>
          </Card>
        ))}

        <div className="sticky bottom-4 flex justify-end pt-4">
          <Button type="submit" isLoading={mutation.isPending}>
            <Save className="mr-2 h-4 w-4" />
            {t('common.saveChanges')}
          </Button>
        </div>
      </form>
    </div>
  );
}
