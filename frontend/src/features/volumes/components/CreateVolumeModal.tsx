/* eslint-disable @typescript-eslint/no-explicit-any */
import { useForm, useFieldArray } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, Trash2 } from 'lucide-react';

import { Modal } from '@/components/ui/Modal';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { useToastStore } from '@/store/toastStore';
import { createVolumeFn } from '../api';
import { type CreateVolumeForm, createVolumeSchema, type CreateVolumeDTO } from '../types';

interface CreateVolumeModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function CreateVolumeModal({ isOpen, onClose }: CreateVolumeModalProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const { register, control, handleSubmit, reset, formState: { errors } } = useForm<CreateVolumeForm>({
    resolver: zodResolver(createVolumeSchema),
    defaultValues: {
      driver: 'local',
      driver_opts: [],
    }
  });

  const { fields, append, remove } = useFieldArray({
    control,
    name: "driver_opts"
  });

  const mutation = useMutation({
    mutationFn: createVolumeFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['volumes'] });
      addToast('Том успешно создан', 'success');
      reset();
      onClose();
    },
    onError: () => {
      addToast('Ошибка при создании тома', 'error');
    },
  });

  const onSubmit = (data: any) => {
    const dto: CreateVolumeDTO = {
      name: data.name,
      driver: data.driver,
    };

    if (data.driver_opts && data.driver_opts.length > 0) {
      dto.driver_opts = data.driver_opts.reduce((acc: Record<string, string>, curr: any) => {
        if (curr.key) acc[curr.key] = curr.value || '';
        return acc;
      }, {});
    }

    mutation.mutate(dto);
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Создать том" className="max-w-2xl">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="name">Имя тома</Label>
            <Input id="name" placeholder="data-volume" error={!!errors.name} {...register('name')} />
            {errors.name && <p className="text-sm text-red-500">{errors.name?.message as string}</p>}
          </div>

          <div className="space-y-2">
            <Label htmlFor="driver">Драйвер</Label>
            <Input id="driver" placeholder="local" error={!!errors.driver} {...register('driver')} />
            {errors.driver && <p className="text-sm text-red-500">{errors.driver?.message as string}</p>}
          </div>
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">Опции драйвера (опционально)</h4>
          {fields.map((field, index) => (
            <div key={field.id} className="flex gap-2 items-start">
              <div className="flex-1">
                <Input placeholder="Ключ (например, type)" {...register(`driver_opts.${index}.key`)} />
              </div>
              <div className="flex-1">
                <Input placeholder="Значение (например, nfs)" {...register(`driver_opts.${index}.value`)} />
              </div>
              <Button type="button" variant="danger" onClick={() => remove(index)} className="px-3">
                <Trash2 className="w-4 h-4" />
              </Button>
            </div>
          ))}
          <Button type="button" variant="secondary" onClick={() => append({ key: '', value: '' })} className="w-full text-sm">
            <Plus className="w-4 h-4 mr-2" /> Добавить опцию
          </Button>
        </div>

        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={onClose}>Отмена</Button>
          <Button type="submit" isLoading={mutation.isPending}>Создать</Button>
        </div>
      </form>
    </Modal>
  );
}