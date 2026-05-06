import { useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useForm, type UseFormRegister } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { Settings, Save } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { useToastStore } from '@/store/toastStore';
import { getSystemConfigFn, updateSystemConfigFn } from '@/features/admin/api';
import { type SystemConfig, type SystemConfigForm, systemConfigSchema } from '@/features/admin/types';
import { getApiErrorMessage } from '@/lib/apiError';

type ConfigFieldName = keyof SystemConfigForm & string;
type ConfigFieldConfig = {
  name: ConfigFieldName;
  label: string;
  type?: 'text' | 'number' | 'float' | 'boolean' | 'array';
};

const sections: Array<{
  title: string;
  fields: ConfigFieldConfig[];
}> = [
  {
    title: 'Runtime resources',
    fields: [
      { name: 'base_domain', label: 'Base domain' },
      { name: 'default_memory_reservation_bytes', label: 'Default memory reservation bytes', type: 'number' },
      { name: 'reserved_system_memory_bytes', label: 'Reserved system memory bytes', type: 'number' },
      { name: 'overcommit_factor', label: 'Overcommit factor', type: 'float' },
      { name: 'max_burst_multiplier', label: 'Max burst multiplier', type: 'number' },
      { name: 'default_cpu_shares', label: 'Default CPU shares', type: 'number' },
      { name: 'high_load_cpu_shares', label: 'High load CPU shares', type: 'number' },
      { name: 'high_load_container_count', label: 'High load container count', type: 'number' },
      { name: 'container_stop_timeout', label: 'Container stop timeout seconds', type: 'number' },
      { name: 'container_ttl_hours', label: 'Container TTL hours', type: 'number' },
      { name: 'container_pids_limit', label: 'Container PIDs limit', type: 'number' },
      { name: 'container_memory_swap_multiplier', label: 'Container memory swap multiplier', type: 'float' },
      { name: 'max_volumes_per_user', label: 'Max volumes per user', type: 'number' },
      { name: 'max_containers_per_user', label: 'Max containers per user', type: 'number' },
      { name: 'container_disk_quota', label: 'Container disk quota' },
      { name: 'max_log_size', label: 'Docker max log size' },
      { name: 'max_log_files', label: 'Docker max log files' },
    ],
  },
  {
    title: 'Registry / Proxy',
    fields: [
      { name: 'registry_api_url', label: 'Registry API URL' },
      { name: 'registry_public_url', label: 'Registry public URL' },
      { name: 'proxy_network_name', label: 'Proxy network name' },
      { name: 'registry_container_name', label: 'Registry container name' },
      { name: 'reserved_domain_prefixes', label: 'Reserved domain prefixes', type: 'array' },
    ],
  },
  {
    title: 'Builds',
    fields: [
      { name: 'image_builds_enabled', label: 'Image builds enabled', type: 'boolean' },
      { name: 'build_memory_bytes', label: 'Build memory bytes', type: 'number' },
      { name: 'build_cpu_quota', label: 'Build CPU quota', type: 'number' },
      { name: 'build_cpu_period', label: 'Build CPU period', type: 'number' },
      { name: 'build_memory_swap_multiplier', label: 'Build memory swap multiplier', type: 'float' },
      { name: 'build_pids_limit', label: 'Build PIDs limit', type: 'number' },
      { name: 'build_network_name', label: 'Build network name' },
      { name: 'kaniko_image', label: 'Kaniko image' },
      { name: 'max_build_time_minutes', label: 'Max build time minutes', type: 'number' },
      { name: 'max_concurrent_builds', label: 'Max concurrent builds', type: 'number' },
      { name: 'max_upload_size_bytes', label: 'Max upload size bytes', type: 'number' },
      { name: 'max_archive_size_bytes', label: 'Max archive size bytes', type: 'number' },
      { name: 'max_unpacked_size_bytes', label: 'Max unpacked size bytes', type: 'number' },
      { name: 'max_build_log_size_bytes', label: 'Max build log size bytes', type: 'number' },
      { name: 'build_cancel_poll_interval_seconds', label: 'Build cancel poll interval seconds', type: 'number' },
    ],
  },
  {
    title: 'Compose',
    fields: [
      { name: 'compose_upload_max_bytes', label: 'Compose upload max bytes', type: 'number' },
      { name: 'compose_pipeline_timeout_minutes', label: 'Compose pipeline timeout minutes', type: 'number' },
      { name: 'compose_build_poll_interval_seconds', label: 'Compose build poll interval seconds', type: 'number' },
      { name: 'compose_dependency_wait_timeout_minutes', label: 'Compose dependency wait timeout minutes', type: 'number' },
      { name: 'compose_dependency_poll_interval_seconds', label: 'Compose dependency poll interval seconds', type: 'number' },
      { name: 'compose_deploy_worker_count', label: 'Compose deploy worker count', type: 'number' },
      { name: 'compose_outbox_interval_seconds', label: 'Compose outbox interval seconds', type: 'number' },
      { name: 'compose_outbox_batch_size', label: 'Compose outbox batch size', type: 'number' },
      { name: 'compose_deploy_max_attempts', label: 'Compose deploy max attempts', type: 'number' },
    ],
  },
  {
    title: 'Git sources',
    fields: [
      { name: 'git_sources_enabled', label: 'Git sources enabled', type: 'boolean' },
      { name: 'git_allowed_hosts', label: 'Git allowed hosts', type: 'array' },
      { name: 'git_clone_timeout_seconds', label: 'Git clone timeout seconds', type: 'number' },
      { name: 'git_max_repository_bytes', label: 'Git max repository bytes', type: 'number' },
    ],
  },
  {
    title: 'Workers',
    fields: [
      { name: 'ttl_worker_interval_seconds', label: 'TTL worker interval seconds', type: 'number' },
      { name: 'gc_worker_interval_minutes', label: 'GC worker interval minutes', type: 'number' },
      { name: 'stale_build_timeout_minutes', label: 'Stale build timeout minutes', type: 'number' },
      { name: 'event_sync_interval_seconds', label: 'Event sync interval seconds', type: 'number' },
      { name: 'event_reconnect_delay_seconds', label: 'Event reconnect delay seconds', type: 'number' },
      { name: 'build_outbox_interval_seconds', label: 'Build outbox interval seconds', type: 'number' },
      { name: 'build_outbox_batch_size', label: 'Build outbox batch size', type: 'number' },
    ],
  },
  {
    title: 'Telemetry',
    fields: [
      { name: 'telemetry_max_log_tail_lines', label: 'Max log tail lines', type: 'number' },
      { name: 'telemetry_max_log_streams_per_user', label: 'Max log streams per user', type: 'number' },
      { name: 'telemetry_max_terminal_sessions_per_user', label: 'Max terminal sessions per user', type: 'number' },
      { name: 'telemetry_terminal_idle_timeout_seconds', label: 'Terminal idle timeout seconds', type: 'number' },
      { name: 'telemetry_terminal_max_duration_seconds', label: 'Terminal max duration seconds', type: 'number' },
      { name: 'telemetry_allowed_exec_commands', label: 'Allowed exec commands', type: 'array' },
      { name: 'telemetry_max_command_args', label: 'Max command args', type: 'number' },
      { name: 'telemetry_max_command_arg_bytes', label: 'Max command arg bytes', type: 'number' },
      { name: 'telemetry_ws_read_limit_bytes', label: 'WS read limit bytes', type: 'number' },
    ],
  },
];

export function SystemSettingsPage() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const { data: config, isLoading } = useQuery({
    queryKey: ['systemConfig'],
    queryFn: getSystemConfigFn,
  });

  const { register, handleSubmit, reset, setValue, watch, formState: { errors } } = useForm<SystemConfigForm>({
    resolver: zodResolver(systemConfigSchema),
  });

  useEffect(() => {
    if (config) {
      reset(config);
    }
  }, [config, reset]);

  const mutation = useMutation({
    mutationFn: updateSystemConfigFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['systemConfig'] });
      addToast('Конфигурация успешно обновлена', 'success');
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, 'Не удалось обновить конфигурацию');
      addToast(message, 'error', { requestId });
    },
  });

  const onSubmit = (data: SystemConfigForm) => {
    mutation.mutate(data as SystemConfig);
  };

  const arrayText = (name: ConfigFieldName) => {
    const value = watch(name);
    return Array.isArray(value) ? value.join('\n') : '';
  };

  if (isLoading) {
    return <div className="h-96 bg-white/40 dark:bg-slate-900/40 rounded-2xl animate-pulse" />;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Настройки системы</h1>
        <p className="text-slate-500 dark:text-slate-400 mt-1">Runtime-конфигурация Core. Сохраняется полный объект без удаления новых полей.</p>
      </div>

      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        {sections.map((section) => (
          <Card key={section.title}>
            <CardHeader>
              <CardTitle className="flex items-center gap-2 text-lg">
                <Settings className="w-5 h-5 text-indigo-600 dark:text-indigo-400" />
                {section.title}
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-5">
                {section.fields.map((field) => (
                  <ConfigField
                    key={field.name}
                    field={field}
                    register={register}
                    error={errors[field.name as keyof SystemConfigForm]}
                    value={field.type === 'array' ? arrayText(field.name) : undefined}
                    onArrayChange={(value) => {
                      setValue(field.name, value.split('\n').map((item) => item.trim()).filter(Boolean) as never, { shouldDirty: true });
                    }}
                  />
                ))}
              </div>
            </CardContent>
          </Card>
        ))}

        <div className="sticky bottom-4 flex justify-end pt-4">
          <Button type="submit" isLoading={mutation.isPending}>
            <Save className="w-4 h-4 mr-2" />
            Сохранить изменения
          </Button>
        </div>
      </form>
    </div>
  );
}

function ConfigField({
  field,
  register,
  error,
  value,
  onArrayChange,
}: {
  field: ConfigFieldConfig;
  register: UseFormRegister<SystemConfigForm>;
  error: unknown;
  value?: string;
  onArrayChange: (value: string) => void;
}) {
  if (field.type === 'boolean') {
    return (
      <label className="flex items-center gap-3 rounded-xl border border-white/40 dark:border-slate-700/50 bg-white/40 dark:bg-slate-900/40 px-4 py-3 text-sm">
        <input type="checkbox" {...register(field.name)} className="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500" />
        <span>{field.label}</span>
      </label>
    );
  }

  if (field.type === 'array') {
    return (
      <div className="space-y-2 xl:col-span-1">
        <Label htmlFor={field.name}>{field.label}</Label>
        <textarea
          id={field.name}
          value={value || ''}
          onChange={(event) => onArrayChange(event.target.value)}
          className="min-h-28 flex w-full rounded-xl bg-white/50 dark:bg-slate-900/50 backdrop-blur-md border border-white/40 dark:border-slate-700/50 px-4 py-2.5 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500"
          placeholder="По одному значению на строку"
        />
      </div>
    );
  }

  const isNumber = field.type === 'number' || field.type === 'float';
  return (
    <div className="space-y-2">
      <Label htmlFor={field.name}>{field.label}</Label>
      <Input
        id={field.name}
        type={isNumber ? 'number' : 'text'}
        step={field.type === 'float' ? '0.1' : undefined}
        error={!!error}
        {...register(field.name, isNumber ? { valueAsNumber: true } : undefined)}
      />
    </div>
  );
}
