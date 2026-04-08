import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { Modal } from '@/components/ui/Modal';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { useToastStore } from '@/store/toastStore';
import { exposeContainerFn } from '../api';
import { type ExposeContainerDTO, exposeContainerSchema, type ContainerData } from '../types';

interface ExposeContainerModalProps {
  container: ContainerData | null;
  onClose: () => void;
}

export function ExposeContainerModal({ container, onClose }: ExposeContainerModalProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const { register, handleSubmit, reset, formState: { errors } } = useForm<ExposeContainerDTO>({
    resolver: zodResolver(exposeContainerSchema),
  });

  useEffect(() => {
    if (container) {
      reset({
        domain_prefix: container.domain_prefix || '',
        internal_port: container.internal_port || 80,
      });
    }
  }, [container, reset]);

  const mutation = useMutation({
    mutationFn: (data: ExposeContainerDTO) => {
      if (!container) throw new Error('No container');
      return exposeContainerFn({ id: container.id, data });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['containers'] });
      addToast('Настройки маршрутизации обновлены', 'success');
      onClose();
    },
    onError: () => {
      addToast('Ошибка при публикации контейнера', 'error');
    },
  });

  const onSubmit = (data: ExposeContainerDTO) => {
    mutation.mutate({
      domain_prefix: data.domain_prefix,
      internal_port: Number(data.internal_port),
    });
  };

  return (
    <Modal isOpen={!!container} onClose={onClose} title={`Публикация: ${container?.name}`}>
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-4 mt-4">
        <div className="space-y-2">
          <Label htmlFor="domain_prefix">Доменный префикс</Label>
          <div className="flex items-center gap-2">
            <Input
              id="domain_prefix"
              placeholder="my-app"
              error={!!errors.domain_prefix}
              {...register('domain_prefix')}
            />
            <span className="text-slate-500 dark:text-slate-400 whitespace-nowrap">.yourdomain.com</span>
          </div>
          {errors.domain_prefix && <p className="text-sm text-red-500">{errors.domain_prefix.message}</p>}
        </div>

        <div className="space-y-2">
          <Label htmlFor="internal_port">Внутренний порт контейнера</Label>
          <Input
            id="internal_port"
            type="number"
            placeholder="80"
            error={!!errors.internal_port}
            {...register('internal_port', { valueAsNumber: true })}
          />
          <p className="text-xs text-slate-500">Порт, на котором приложение слушает внутри контейнера</p>
          {errors.internal_port && <p className="text-sm text-red-500">{errors.internal_port.message}</p>}
        </div>

        <div className="bg-blue-50 dark:bg-blue-900/20 p-4 rounded-xl border border-blue-100 dark:border-blue-900/50 mt-4 text-sm text-blue-800 dark:text-blue-300">
          Контейнер будет пересоздан для применения новых сетевых настроек. Это может занять несколько секунд.
        </div>

        <div className="flex justify-end gap-3 pt-6 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={onClose}>Отмена</Button>
          <Button type="submit" isLoading={mutation.isPending}>Сохранить</Button>
        </div>
      </form>
    </Modal>
  );
}