import { useEffect, useState, type KeyboardEvent } from 'react';
import { useForm, type UseFormRegister, type UseFormSetValue, type UseFormWatch } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { Info, Plus, RefreshCcw, Save, Settings, X } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { Select } from '@/components/ui/Select';
import { type SystemConfig, type SystemConfigForm, systemConfigSchema } from '@/features/admin/types';
import { getApiErrorMessage } from '@/lib/apiError';
import { type TFunction, type TranslationKey, useT } from '@/lib/i18n';
import { cn } from '@/lib/utils';
import { useSystemConfig, useUpdateSystemConfig } from '@/features/admin/hooks';
import { useAuthConfig } from '@/features/auth/hooks';
import type { AuthConfig } from '@/features/auth/types';

type ConfigFieldName = keyof SystemConfigForm & string;
type ConfigFieldType = 'text' | 'number' | 'float' | 'boolean' | 'array' | 'bytes';
type ConfigFieldConfig = {
  name: ConfigFieldName;
  labelKey: TranslationKey;
  hintKey: TranslationKey;
  type?: ConfigFieldType;
  unitKey?: TranslationKey;
  placeholderKey?: TranslationKey;
};

const sections: Array<{
  titleKey: TranslationKey;
  fields: ConfigFieldConfig[];
}> = [
  {
    titleKey: 'admin.settings.section.runtime',
    fields: [
      { name: 'base_domain', labelKey: 'admin.settings.field.base_domain.label', hintKey: 'admin.settings.field.base_domain.hint', placeholderKey: 'admin.settings.placeholder.domain' },
      { name: 'default_memory_reservation_bytes', labelKey: 'admin.settings.field.default_memory_reservation_bytes.label', hintKey: 'admin.settings.field.default_memory_reservation_bytes.hint', type: 'bytes' },
      { name: 'reserved_system_memory_bytes', labelKey: 'admin.settings.field.reserved_system_memory_bytes.label', hintKey: 'admin.settings.field.reserved_system_memory_bytes.hint', type: 'bytes' },
      { name: 'overcommit_factor', labelKey: 'admin.settings.field.overcommit_factor.label', hintKey: 'admin.settings.field.overcommit_factor.hint', type: 'float' },
      { name: 'max_burst_multiplier', labelKey: 'admin.settings.field.max_burst_multiplier.label', hintKey: 'admin.settings.field.max_burst_multiplier.hint', type: 'number' },
      { name: 'default_cpu_reservation_millicores', labelKey: 'admin.settings.field.default_cpu_reservation_millicores.label', hintKey: 'admin.settings.field.default_cpu_reservation_millicores.hint', type: 'number' },
      { name: 'reserved_system_cpu_millicores', labelKey: 'admin.settings.field.reserved_system_cpu_millicores.label', hintKey: 'admin.settings.field.reserved_system_cpu_millicores.hint', type: 'number' },
      { name: 'cpu_overcommit_factor', labelKey: 'admin.settings.field.cpu_overcommit_factor.label', hintKey: 'admin.settings.field.cpu_overcommit_factor.hint', type: 'float' },
      { name: 'max_cpu_burst_multiplier', labelKey: 'admin.settings.field.max_cpu_burst_multiplier.label', hintKey: 'admin.settings.field.max_cpu_burst_multiplier.hint', type: 'number' },
      { name: 'container_cpu_period', labelKey: 'admin.settings.field.container_cpu_period.label', hintKey: 'admin.settings.field.container_cpu_period.hint', type: 'number' },
      { name: 'default_cpu_shares', labelKey: 'admin.settings.field.default_cpu_shares.label', hintKey: 'admin.settings.field.default_cpu_shares.hint', type: 'number' },
      { name: 'high_load_cpu_shares', labelKey: 'admin.settings.field.high_load_cpu_shares.label', hintKey: 'admin.settings.field.high_load_cpu_shares.hint', type: 'number' },
      { name: 'high_load_container_count', labelKey: 'admin.settings.field.high_load_container_count.label', hintKey: 'admin.settings.field.high_load_container_count.hint', type: 'number' },
      { name: 'container_stop_timeout', labelKey: 'admin.settings.field.container_stop_timeout.label', hintKey: 'admin.settings.field.container_stop_timeout.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'container_ttl_hours', labelKey: 'admin.settings.field.container_ttl_hours.label', hintKey: 'admin.settings.field.container_ttl_hours.hint', type: 'number', unitKey: 'admin.settings.unit.hours' },
      { name: 'container_pids_limit', labelKey: 'admin.settings.field.container_pids_limit.label', hintKey: 'admin.settings.field.container_pids_limit.hint', type: 'number' },
      { name: 'container_memory_swap_multiplier', labelKey: 'admin.settings.field.container_memory_swap_multiplier.label', hintKey: 'admin.settings.field.container_memory_swap_multiplier.hint', type: 'float' },
      { name: 'max_volumes_per_user', labelKey: 'admin.settings.field.max_volumes_per_user.label', hintKey: 'admin.settings.field.max_volumes_per_user.hint', type: 'number' },
      { name: 'max_containers_per_user', labelKey: 'admin.settings.field.max_containers_per_user.label', hintKey: 'admin.settings.field.max_containers_per_user.hint', type: 'number' },
      { name: 'container_disk_quota', labelKey: 'admin.settings.field.container_disk_quota.label', hintKey: 'admin.settings.field.container_disk_quota.hint', placeholderKey: 'admin.settings.placeholder.dockerSize' },
      { name: 'max_log_size', labelKey: 'admin.settings.field.max_log_size.label', hintKey: 'admin.settings.field.max_log_size.hint', placeholderKey: 'admin.settings.placeholder.logSize' },
      { name: 'max_log_files', labelKey: 'admin.settings.field.max_log_files.label', hintKey: 'admin.settings.field.max_log_files.hint', placeholderKey: 'admin.settings.placeholder.logFiles' },
    ],
  },
  {
    titleKey: 'admin.settings.section.registry',
    fields: [
      { name: 'reserved_domain_prefixes', labelKey: 'admin.settings.field.reserved_domain_prefixes.label', hintKey: 'admin.settings.field.reserved_domain_prefixes.hint', type: 'array', placeholderKey: 'admin.settings.placeholder.addPrefix' },
    ],
  },
  {
    titleKey: 'admin.settings.section.builds',
    fields: [
      { name: 'image_builds_enabled', labelKey: 'admin.settings.field.image_builds_enabled.label', hintKey: 'admin.settings.field.image_builds_enabled.hint', type: 'boolean' },
      { name: 'build_memory_bytes', labelKey: 'admin.settings.field.build_memory_bytes.label', hintKey: 'admin.settings.field.build_memory_bytes.hint', type: 'bytes' },
      { name: 'build_cpu_quota', labelKey: 'admin.settings.field.build_cpu_quota.label', hintKey: 'admin.settings.field.build_cpu_quota.hint', type: 'number' },
      { name: 'build_cpu_period', labelKey: 'admin.settings.field.build_cpu_period.label', hintKey: 'admin.settings.field.build_cpu_period.hint', type: 'number' },
      { name: 'build_memory_swap_multiplier', labelKey: 'admin.settings.field.build_memory_swap_multiplier.label', hintKey: 'admin.settings.field.build_memory_swap_multiplier.hint', type: 'float' },
      { name: 'build_pids_limit', labelKey: 'admin.settings.field.build_pids_limit.label', hintKey: 'admin.settings.field.build_pids_limit.hint', type: 'number' },
      { name: 'kaniko_image', labelKey: 'admin.settings.field.kaniko_image.label', hintKey: 'admin.settings.field.kaniko_image.hint' },
      { name: 'max_build_time_minutes', labelKey: 'admin.settings.field.max_build_time_minutes.label', hintKey: 'admin.settings.field.max_build_time_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'max_concurrent_builds', labelKey: 'admin.settings.field.max_concurrent_builds.label', hintKey: 'admin.settings.field.max_concurrent_builds.hint', type: 'number' },
      { name: 'max_upload_size_bytes', labelKey: 'admin.settings.field.max_upload_size_bytes.label', hintKey: 'admin.settings.field.max_upload_size_bytes.hint', type: 'bytes' },
      { name: 'max_archive_size_bytes', labelKey: 'admin.settings.field.max_archive_size_bytes.label', hintKey: 'admin.settings.field.max_archive_size_bytes.hint', type: 'bytes' },
      { name: 'max_unpacked_size_bytes', labelKey: 'admin.settings.field.max_unpacked_size_bytes.label', hintKey: 'admin.settings.field.max_unpacked_size_bytes.hint', type: 'bytes' },
      { name: 'max_build_log_size_bytes', labelKey: 'admin.settings.field.max_build_log_size_bytes.label', hintKey: 'admin.settings.field.max_build_log_size_bytes.hint', type: 'bytes' },
      { name: 'build_cancel_poll_interval_seconds', labelKey: 'admin.settings.field.build_cancel_poll_interval_seconds.label', hintKey: 'admin.settings.field.build_cancel_poll_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
    ],
  },
  {
    titleKey: 'admin.settings.section.compose',
    fields: [
      { name: 'compose_upload_max_bytes', labelKey: 'admin.settings.field.compose_upload_max_bytes.label', hintKey: 'admin.settings.field.compose_upload_max_bytes.hint', type: 'bytes' },
      { name: 'compose_pipeline_timeout_minutes', labelKey: 'admin.settings.field.compose_pipeline_timeout_minutes.label', hintKey: 'admin.settings.field.compose_pipeline_timeout_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'compose_coordinator_interval_seconds', labelKey: 'admin.settings.field.compose_coordinator_interval_seconds.label', hintKey: 'admin.settings.field.compose_coordinator_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'compose_build_poll_interval_seconds', labelKey: 'admin.settings.field.compose_build_poll_interval_seconds.label', hintKey: 'admin.settings.field.compose_build_poll_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'compose_dependency_wait_timeout_minutes', labelKey: 'admin.settings.field.compose_dependency_wait_timeout_minutes.label', hintKey: 'admin.settings.field.compose_dependency_wait_timeout_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'compose_dependency_poll_interval_seconds', labelKey: 'admin.settings.field.compose_dependency_poll_interval_seconds.label', hintKey: 'admin.settings.field.compose_dependency_poll_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'compose_deploy_worker_count', labelKey: 'admin.settings.field.compose_deploy_worker_count.label', hintKey: 'admin.settings.field.compose_deploy_worker_count.hint', type: 'number' },
      { name: 'compose_outbox_interval_seconds', labelKey: 'admin.settings.field.compose_outbox_interval_seconds.label', hintKey: 'admin.settings.field.compose_outbox_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'compose_outbox_batch_size', labelKey: 'admin.settings.field.compose_outbox_batch_size.label', hintKey: 'admin.settings.field.compose_outbox_batch_size.hint', type: 'number' },
      { name: 'compose_deploy_max_attempts', labelKey: 'admin.settings.field.compose_deploy_max_attempts.label', hintKey: 'admin.settings.field.compose_deploy_max_attempts.hint', type: 'number' },
      { name: 'max_queued_compose_deploys_per_user', labelKey: 'admin.settings.field.max_queued_compose_deploys_per_user.label', hintKey: 'admin.settings.field.max_queued_compose_deploys_per_user.hint', type: 'number' },
    ],
  },
  {
    titleKey: 'admin.settings.section.git',
    fields: [
      { name: 'git_sources_enabled', labelKey: 'admin.settings.field.git_sources_enabled.label', hintKey: 'admin.settings.field.git_sources_enabled.hint', type: 'boolean' },
      { name: 'git_allowed_hosts', labelKey: 'admin.settings.field.git_allowed_hosts.label', hintKey: 'admin.settings.field.git_allowed_hosts.hint', type: 'array', placeholderKey: 'admin.settings.placeholder.addHost' },
      { name: 'git_clone_timeout_seconds', labelKey: 'admin.settings.field.git_clone_timeout_seconds.label', hintKey: 'admin.settings.field.git_clone_timeout_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'git_max_repository_bytes', labelKey: 'admin.settings.field.git_max_repository_bytes.label', hintKey: 'admin.settings.field.git_max_repository_bytes.hint', type: 'bytes' },
    ],
  },
  {
    titleKey: 'admin.settings.section.workers',
    fields: [
      { name: 'ttl_worker_interval_seconds', labelKey: 'admin.settings.field.ttl_worker_interval_seconds.label', hintKey: 'admin.settings.field.ttl_worker_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'gc_worker_interval_minutes', labelKey: 'admin.settings.field.gc_worker_interval_minutes.label', hintKey: 'admin.settings.field.gc_worker_interval_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'stale_build_timeout_minutes', labelKey: 'admin.settings.field.stale_build_timeout_minutes.label', hintKey: 'admin.settings.field.stale_build_timeout_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'event_sync_interval_seconds', labelKey: 'admin.settings.field.event_sync_interval_seconds.label', hintKey: 'admin.settings.field.event_sync_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'event_reconnect_delay_seconds', labelKey: 'admin.settings.field.event_reconnect_delay_seconds.label', hintKey: 'admin.settings.field.event_reconnect_delay_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'build_outbox_interval_seconds', labelKey: 'admin.settings.field.build_outbox_interval_seconds.label', hintKey: 'admin.settings.field.build_outbox_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'build_outbox_batch_size', labelKey: 'admin.settings.field.build_outbox_batch_size.label', hintKey: 'admin.settings.field.build_outbox_batch_size.hint', type: 'number' },
      { name: 'container_create_worker_count', labelKey: 'admin.settings.field.container_create_worker_count.label', hintKey: 'admin.settings.field.container_create_worker_count.hint', type: 'number' },
      { name: 'container_create_max_attempts', labelKey: 'admin.settings.field.container_create_max_attempts.label', hintKey: 'admin.settings.field.container_create_max_attempts.hint', type: 'number' },
      { name: 'container_create_timeout_minutes', labelKey: 'admin.settings.field.container_create_timeout_minutes.label', hintKey: 'admin.settings.field.container_create_timeout_minutes.hint', type: 'number', unitKey: 'admin.settings.unit.minutes' },
      { name: 'container_create_outbox_interval_seconds', labelKey: 'admin.settings.field.container_create_outbox_interval_seconds.label', hintKey: 'admin.settings.field.container_create_outbox_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'container_create_outbox_batch_size', labelKey: 'admin.settings.field.container_create_outbox_batch_size.label', hintKey: 'admin.settings.field.container_create_outbox_batch_size.hint', type: 'number' },
      { name: 'reports_usage_snapshot_interval_seconds', labelKey: 'admin.settings.field.reports_usage_snapshot_interval_seconds.label', hintKey: 'admin.settings.field.reports_usage_snapshot_interval_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
    ],
  },
  {
    titleKey: 'admin.settings.section.safety',
    fields: [
      { name: 'max_queued_container_creates_per_user', labelKey: 'admin.settings.field.max_queued_container_creates_per_user.label', hintKey: 'admin.settings.field.max_queued_container_creates_per_user.hint', type: 'number' },
      { name: 'max_queued_builds_per_user', labelKey: 'admin.settings.field.max_queued_builds_per_user.label', hintKey: 'admin.settings.field.max_queued_builds_per_user.hint', type: 'number' },
      { name: 'max_staged_source_bytes_per_user', labelKey: 'admin.settings.field.max_staged_source_bytes_per_user.label', hintKey: 'admin.settings.field.max_staged_source_bytes_per_user.hint', type: 'bytes' },
      { name: 'host_min_free_disk_bytes', labelKey: 'admin.settings.field.host_min_free_disk_bytes.label', hintKey: 'admin.settings.field.host_min_free_disk_bytes.hint', type: 'bytes' },
    ],
  },
  {
    titleKey: 'admin.settings.section.telemetry',
    fields: [
      { name: 'telemetry_max_log_tail_lines', labelKey: 'admin.settings.field.telemetry_max_log_tail_lines.label', hintKey: 'admin.settings.field.telemetry_max_log_tail_lines.hint', type: 'number', unitKey: 'admin.settings.unit.lines' },
      { name: 'telemetry_max_log_streams_per_user', labelKey: 'admin.settings.field.telemetry_max_log_streams_per_user.label', hintKey: 'admin.settings.field.telemetry_max_log_streams_per_user.hint', type: 'number' },
      { name: 'telemetry_max_terminal_sessions_per_user', labelKey: 'admin.settings.field.telemetry_max_terminal_sessions_per_user.label', hintKey: 'admin.settings.field.telemetry_max_terminal_sessions_per_user.hint', type: 'number' },
      { name: 'telemetry_terminal_idle_timeout_seconds', labelKey: 'admin.settings.field.telemetry_terminal_idle_timeout_seconds.label', hintKey: 'admin.settings.field.telemetry_terminal_idle_timeout_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'telemetry_terminal_max_duration_seconds', labelKey: 'admin.settings.field.telemetry_terminal_max_duration_seconds.label', hintKey: 'admin.settings.field.telemetry_terminal_max_duration_seconds.hint', type: 'number', unitKey: 'admin.settings.unit.seconds' },
      { name: 'telemetry_allowed_exec_commands', labelKey: 'admin.settings.field.telemetry_allowed_exec_commands.label', hintKey: 'admin.settings.field.telemetry_allowed_exec_commands.hint', type: 'array', placeholderKey: 'admin.settings.placeholder.addCommand' },
      { name: 'telemetry_max_command_args', labelKey: 'admin.settings.field.telemetry_max_command_args.label', hintKey: 'admin.settings.field.telemetry_max_command_args.hint', type: 'number' },
      { name: 'telemetry_max_command_arg_bytes', labelKey: 'admin.settings.field.telemetry_max_command_arg_bytes.label', hintKey: 'admin.settings.field.telemetry_max_command_arg_bytes.hint', type: 'bytes' },
      { name: 'telemetry_ws_read_limit_bytes', labelKey: 'admin.settings.field.telemetry_ws_read_limit_bytes.label', hintKey: 'admin.settings.field.telemetry_ws_read_limit_bytes.hint', type: 'bytes' },
    ],
  },
];

const byteUnits = [
  { label: 'Bytes', multiplier: 1 },
  { label: 'KB', multiplier: 1024 },
  { label: 'MB', multiplier: 1024 ** 2 },
  { label: 'GB', multiplier: 1024 ** 3 },
] as const;

type ByteUnit = typeof byteUnits[number]['label'];

function preferredByteUnit(bytes: number): ByteUnit {
  if (bytes >= 1024 ** 3 && bytes % (1024 ** 3) === 0) return 'GB';
  if (bytes >= 1024 ** 2 && bytes % (1024 ** 2) === 0) return 'MB';
  if (bytes >= 1024 && bytes % 1024 === 0) return 'KB';
  return 'Bytes';
}

function getErrorMessage(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message;
  }
  return undefined;
}

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

function AuthSettingsStatus({
  authConfig,
  isLoading,
  isError,
  t,
}: {
  authConfig?: AuthConfig;
  isLoading: boolean;
  isError: boolean;
  t: TFunction;
}) {
  const oidcProviders = authConfig?.oidc_providers.map((provider) => provider.name).filter(Boolean) ?? [];
  const externalSSOEnabled = oidcProviders.length > 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-lg">
          <Settings className="h-5 w-5 text-indigo-600 dark:text-indigo-400" />
          {t('admin.settings.section.auth')}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="h-24 rounded-xl bg-white/40 animate-pulse dark:bg-slate-900/40" />
        ) : isError ? (
          <p className="text-sm text-red-600 dark:text-red-400">{t('admin.settings.authStatusUnavailable')}</p>
        ) : (
          <div className="grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3">
            <ReadOnlyStatusField
              label={t('admin.settings.field.local_login_enabled.label')}
              hint={t('admin.settings.field.local_login_enabled.hint')}
              enabled={authConfig?.local_login_enabled ?? false}
              t={t}
            />
            <ReadOnlyStatusField
              label={t('admin.settings.field.local_register_enabled.label')}
              hint={t('admin.settings.field.local_register_enabled.hint')}
              enabled={authConfig?.local_register_enabled ?? false}
              t={t}
            />
            <ReadOnlyStatusField
              label={t('admin.settings.field.external_sso_enabled.label')}
              hint={t('admin.settings.field.external_sso_enabled.hint', {
                providers: externalSSOEnabled ? oidcProviders.join(', ') : t('admin.settings.none'),
              })}
              enabled={externalSSOEnabled}
              t={t}
            />
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function ReadOnlyStatusField({
  label,
  hint,
  enabled,
  t,
}: {
  label: string;
  hint: string;
  enabled: boolean;
  t: TFunction;
}) {
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Label>{label}</Label>
        <InfoHint text={hint} label={label} t={t} />
      </div>
      <div
        className={cn(
          'flex w-full items-center justify-between rounded-xl border px-4 py-3 text-left text-sm',
          enabled
            ? 'border-indigo-300 bg-indigo-50 text-indigo-900 dark:border-indigo-800 dark:bg-indigo-950/40 dark:text-indigo-100'
            : 'border-white/40 bg-white/40 text-slate-700 dark:border-slate-700/50 dark:bg-slate-900/40 dark:text-slate-300'
        )}
      >
        <span className="font-medium">{enabled ? t('admin.settings.enabled') : t('admin.settings.disabled')}</span>
        <span className={cn('relative h-6 w-11 rounded-full', enabled ? 'bg-indigo-600' : 'bg-slate-300 dark:bg-slate-700')}>
          <span className={cn('absolute top-1 h-4 w-4 rounded-full bg-white shadow', enabled ? 'translate-x-6' : 'translate-x-1')} />
        </span>
      </div>
    </div>
  );
}

function ConfigField({
  field,
  register,
  setValue,
  watch,
  error,
  t,
}: {
  field: ConfigFieldConfig;
  register: UseFormRegister<SystemConfigForm>;
  setValue: UseFormSetValue<SystemConfigForm>;
  watch: UseFormWatch<SystemConfigForm>;
  error: unknown;
  t: TFunction;
}) {
  if (field.type === 'boolean') {
    return <SwitchField field={field} setValue={setValue} watch={watch} error={error} t={t} />;
  }

  if (field.type === 'array') {
    return <ListField field={field} setValue={setValue} watch={watch} error={error} t={t} />;
  }

  if (field.type === 'bytes') {
    return <ByteField field={field} setValue={setValue} watch={watch} error={error} t={t} />;
  }

  const isNumber = field.type === 'number' || field.type === 'float';
  const errorMessage = getErrorMessage(error);

  return (
    <div className="space-y-2">
      <FieldLabel field={field} t={t} />
      <div className="relative">
        <Input
          id={field.name}
          type={isNumber ? 'number' : 'text'}
          step={field.type === 'float' ? '0.1' : undefined}
          placeholder={field.placeholderKey ? t(field.placeholderKey) : undefined}
          error={!!error}
          className={field.unitKey ? 'pr-24' : undefined}
          {...register(field.name, isNumber ? { valueAsNumber: true } : undefined)}
        />
        {field.unitKey && (
          <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs font-medium text-slate-500 dark:text-slate-400">
            {t(field.unitKey)}
          </span>
        )}
      </div>
      <FieldError message={errorMessage} />
    </div>
  );
}

function FieldLabel({ field, t }: { field: ConfigFieldConfig; t: TFunction }) {
  return (
    <div className="flex items-center gap-2">
      <Label htmlFor={field.name}>{t(field.labelKey)}</Label>
      <InfoHint text={t(field.hintKey)} label={t(field.labelKey)} t={t} />
    </div>
  );
}

function InfoHint({ text, label, t }: { text: string; label: string; t: TFunction }) {
  const [open, setOpen] = useState(false);

  return (
    <span className="relative inline-flex">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        onBlur={() => window.setTimeout(() => setOpen(false), 120)}
        className="inline-flex h-5 w-5 items-center justify-center rounded-full border border-slate-300 bg-white/70 text-slate-500 transition-colors hover:text-indigo-600 focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:border-slate-600 dark:bg-slate-900/70 dark:text-slate-400 dark:hover:text-indigo-300"
        aria-label={t('admin.settings.showHint', { label })}
        aria-expanded={open}
      >
        <Info className="h-3.5 w-3.5" />
      </button>
      {open && (
        <span className="absolute left-1/2 top-7 z-20 w-72 -translate-x-1/2 rounded-lg border border-slate-200 bg-white p-3 text-xs leading-5 text-slate-600 shadow-xl dark:border-slate-700 dark:bg-slate-950 dark:text-slate-300">
          {text}
        </span>
      )}
    </span>
  );
}

function FieldError({ message }: { message?: string }) {
  if (!message) return null;
  return <p className="text-xs font-medium text-red-600 dark:text-red-400">{message}</p>;
}

function SwitchField({
  field,
  setValue,
  watch,
  error,
  t,
}: {
  field: ConfigFieldConfig;
  setValue: UseFormSetValue<SystemConfigForm>;
  watch: UseFormWatch<SystemConfigForm>;
  error: unknown;
  t: TFunction;
}) {
  const checked = Boolean(watch(field.name));
  const errorMessage = getErrorMessage(error);

  return (
    <div className="space-y-2">
      <FieldLabel field={field} t={t} />
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => setValue(field.name, !checked as never, { shouldDirty: true, shouldValidate: true })}
        className={cn(
          'flex w-full items-center justify-between rounded-xl border px-4 py-3 text-left text-sm transition-colors focus:outline-none focus:ring-2 focus:ring-indigo-500',
          checked
            ? 'border-indigo-300 bg-indigo-50 text-indigo-900 dark:border-indigo-800 dark:bg-indigo-950/40 dark:text-indigo-100'
            : 'border-white/40 bg-white/40 text-slate-700 dark:border-slate-700/50 dark:bg-slate-900/40 dark:text-slate-300'
        )}
      >
        <span className="font-medium">{checked ? t('admin.settings.enabled') : t('admin.settings.disabled')}</span>
        <span
          className={cn(
            'relative h-6 w-11 rounded-full transition-colors',
            checked ? 'bg-indigo-600' : 'bg-slate-300 dark:bg-slate-700'
          )}
        >
          <span
            className={cn(
              'absolute top-1 h-4 w-4 rounded-full bg-white shadow transition-transform',
              checked ? 'translate-x-6' : 'translate-x-1'
            )}
          />
        </span>
      </button>
      <FieldError message={errorMessage} />
    </div>
  );
}

function ByteField({
  field,
  setValue,
  watch,
  error,
  t,
}: {
  field: ConfigFieldConfig;
  setValue: UseFormSetValue<SystemConfigForm>;
  watch: UseFormWatch<SystemConfigForm>;
  error: unknown;
  t: TFunction;
}) {
  const bytes = Number(watch(field.name) || 0);
  const [unit, setUnit] = useState<ByteUnit>(() => preferredByteUnit(bytes));
  const [unitInitialized, setUnitInitialized] = useState(false);
  const currentUnit = byteUnits.find((item) => item.label === unit) ?? byteUnits[0];
  const displayValue = bytes / currentUnit.multiplier;
  const errorMessage = getErrorMessage(error);

  useEffect(() => {
    setUnitInitialized(false);
  }, [field.name]);

  useEffect(() => {
    if (!unitInitialized && bytes > 0) {
      setUnit(preferredByteUnit(bytes));
      setUnitInitialized(true);
    }
  }, [bytes, unitInitialized]);

  return (
    <div className="space-y-2">
      <FieldLabel field={field} t={t} />
      <div className="flex gap-2">
        <Input
          id={field.name}
          type="number"
          min={0}
          step={unit === 'Bytes' ? 1 : 0.01}
          value={Number.isFinite(displayValue) ? displayValue : 0}
          error={!!error}
          onChange={(event) => {
            const value = Number(event.target.value);
            setUnitInitialized(true);
            setValue(field.name, Math.round(value * currentUnit.multiplier) as never, { shouldDirty: true, shouldValidate: true });
          }}
        />
        <Select
          value={unit}
          onChange={(event) => {
            setUnitInitialized(true);
            setUnit(event.target.value as ByteUnit);
          }}
          className="w-28 shrink-0"
          aria-label={t('admin.settings.byteUnit')}
        >
          {byteUnits.map((item) => (
            <option key={item.label} value={item.label}>{item.label}</option>
          ))}
        </Select>
      </div>
      <p className="text-xs text-slate-500 dark:text-slate-400">
        {t('admin.settings.rawBytes', { value: bytes })}
      </p>
      <FieldError message={errorMessage} />
    </div>
  );
}

function ListField({
  field,
  setValue,
  watch,
  error,
  t,
}: {
  field: ConfigFieldConfig;
  setValue: UseFormSetValue<SystemConfigForm>;
  watch: UseFormWatch<SystemConfigForm>;
  error: unknown;
  t: TFunction;
}) {
  const items = Array.isArray(watch(field.name)) ? watch(field.name) as string[] : [];
  const [draft, setDraft] = useState('');
  const errorMessage = getErrorMessage(error);

  const updateItems = (nextItems: string[]) => {
    setValue(field.name, nextItems as never, { shouldDirty: true, shouldValidate: true });
  };
  const addDraft = (raw: string) => {
    const values = raw
      .split(/[\n,]+/)
      .map((item) => item.trim())
      .filter(Boolean);

    if (!values.length) return;

    updateItems([...items, ...values.filter((item) => !items.includes(item))]);
    setDraft('');
  };
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault();
      addDraft(draft);
    }
  };

  return (
    <div className="space-y-2">
      <FieldLabel field={field} t={t} />
      <div className="rounded-xl border border-white/40 bg-white/40 p-3 dark:border-slate-700/50 dark:bg-slate-900/40">
        <div className="flex min-h-10 flex-wrap gap-2">
          {items.length ? items.map((item) => (
            <span key={item} className="inline-flex max-w-full items-center gap-1 rounded-lg bg-slate-100 px-2.5 py-1 text-sm text-slate-700 dark:bg-slate-800 dark:text-slate-200">
              <span className="truncate">{item}</span>
              <button
                type="button"
                onClick={() => updateItems(items.filter((current) => current !== item))}
                className="rounded p-0.5 text-slate-500 hover:bg-slate-200 hover:text-red-600 dark:hover:bg-slate-700"
                aria-label={t('admin.settings.removeListItem', { item })}
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </span>
          )) : (
            <span className="py-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.settings.emptyList')}</span>
          )}
        </div>
        <div className="mt-3 flex gap-2">
          <Input
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={onKeyDown}
            onPaste={(event) => {
              const pasted = event.clipboardData.getData('text');
              if (pasted.includes('\n') || pasted.includes(',')) {
                event.preventDefault();
                addDraft(pasted);
              }
            }}
            placeholder={field.placeholderKey ? t(field.placeholderKey) : t('admin.settings.addListItem')}
            error={!!error}
          />
          <Button type="button" variant="secondary" onClick={() => addDraft(draft)} className="shrink-0 px-3">
            <Plus className="h-4 w-4" />
          </Button>
        </div>
      </div>
      <FieldError message={errorMessage} />
    </div>
  );
}
