/* eslint-disable @typescript-eslint/no-explicit-any */
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { Settings, Save } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { useToastStore } from '@/store/toastStore';
import { getSystemConfigFn, updateSystemConfigFn } from '@/features/admin/api';
import { type SystemConfigForm, systemConfigSchema } from '@/features/admin/types';
import { useEffect } from 'react';

export function SystemSettingsPage() {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const { data: config, isLoading } = useQuery({
    queryKey: ['systemConfig'],
    queryFn: getSystemConfigFn,
  });

  const { register, handleSubmit, reset, formState: { errors } } = useForm<SystemConfigForm>({
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
    onError: () => {
      addToast('Ошибка при обновлении конфигурации', 'error');
    },
  });

  const onSubmit = (data: SystemConfigForm) => {
    mutation.mutate(data as any);
  };

  if (isLoading) {
    return <div className="h-96 bg-white/40 dark:bg-slate-900/40 rounded-2xl animate-pulse" />;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold tracking-tight">Настройки системы</h1>
        <p className="text-slate-500 dark:text-slate-400 mt-1">Глобальная конфигурация платформы (dcm/config.json)</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Settings className="w-5 h-5 text-indigo-600 dark:text-indigo-400" />
            Параметры
          </CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-8">
            
            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-6">
              {/* Базовые */}
              <div className="space-y-2">
                <Label htmlFor="base_domain">Базовый домен (Base Domain)</Label>
                <Input id="base_domain" {...register('base_domain')} error={!!errors.base_domain} />
                <p className="text-xs text-slate-500">Домен для проксирования (ex: yourdomain.com)</p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="registry_url">Registry URL</Label>
                <Input id="registry_url" {...register('registry_url')} error={!!errors.registry_url} />
                <p className="text-xs text-slate-500">Хост или контейнер локального Docker Registry</p>
              </div>

              {/* Память */}
              <div className="space-y-2">
                <Label htmlFor="default_memory_reservation_bytes">Дефолтная память (Bytes)</Label>
                <Input id="default_memory_reservation_bytes" type="number" {...register('default_memory_reservation_bytes', { valueAsNumber: true })} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="reserved_system_memory_bytes">Резерв хоста (Bytes)</Label>
                <Input id="reserved_system_memory_bytes" type="number" {...register('reserved_system_memory_bytes', { valueAsNumber: true })} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="overcommit_factor">Оверкоммит фактор (Float)</Label>
                <Input id="overcommit_factor" type="number" step="0.1" {...register('overcommit_factor', { valueAsNumber: true })} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="max_burst_multiplier">Макс. Burst (Множитель)</Label>
                <Input id="max_burst_multiplier" type="number" {...register('max_burst_multiplier', { valueAsNumber: true })} />
              </div>

              {/* Процессор */}
              <div className="space-y-2">
                <Label htmlFor="default_cpu_shares">Дефолтный приоритет CPU (Shares)</Label>
                <Input id="default_cpu_shares" type="number" {...register('default_cpu_shares', { valueAsNumber: true })} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="high_load_cpu_shares">Приоритет под нагрузкой (Shares)</Label>
                <Input id="high_load_cpu_shares" type="number" {...register('high_load_cpu_shares', { valueAsNumber: true })} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="high_load_container_count">Порог высокой нагрузки (Кол-во)</Label>
                <Input id="high_load_container_count" type="number" {...register('high_load_container_count', { valueAsNumber: true })} />
              </div>

              {/* Лимиты пользователя */}
              <div className="space-y-2">
                <Label htmlFor="max_volumes_per_user">Макс. томов на юзера</Label>
                <Input id="max_volumes_per_user" type="number" {...register('max_volumes_per_user', { valueAsNumber: true })} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="max_containers_per_user">Макс. контейнеров на юзера</Label>
                <Input id="max_containers_per_user" type="number" {...register('max_containers_per_user', { valueAsNumber: true })} />
              </div>

              {/* Логи и Диск */}
              <div className="space-y-2">
                <Label htmlFor="max_log_size">Размер лог-файла (Docker)</Label>
                <Input id="max_log_size" {...register('max_log_size')} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="max_log_files">Кол-во лог-файлов</Label>
                <Input id="max_log_files" {...register('max_log_files')} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="container_disk_quota">Дисковая квота контейнера (Docker)</Label>
                <Input id="container_disk_quota" {...register('container_disk_quota')} />
              </div>

              {/* Прочее */}
              <div className="space-y-2">
                <Label htmlFor="container_stop_timeout">Таймаут остановки (Сек)</Label>
                <Input id="container_stop_timeout" type="number" {...register('container_stop_timeout', { valueAsNumber: true })} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="container_ttl_hours">TTL Контейнера (Часы)</Label>
                <Input id="container_ttl_hours" type="number" {...register('container_ttl_hours', { valueAsNumber: true })} />
                <p className="text-xs text-slate-500">0 - бесконечно (отключено)</p>
              </div>
            </div>

            <div className="flex justify-end pt-6 border-t border-slate-200 dark:border-slate-700/50">
              <Button type="submit" isLoading={mutation.isPending}>
                <Save className="w-4 h-4 mr-2" />
                Сохранить изменения
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}