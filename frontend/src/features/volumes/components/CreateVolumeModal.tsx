import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';

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

  const { register, handleSubmit, reset, formState: { errors } } = useForm<CreateVolumeForm>({
    resolver: zodResolver(createVolumeSchema),
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

  const onSubmit = (data: CreateVolumeForm) => {
    const dto: CreateVolumeDTO = {
      name: data.name,
    };
    mutation.mutate(dto);
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Создать том" className="max-w-lg">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        <div className="space-y-2">
          <Label htmlFor="name">Имя тома</Label>
          <Input id="name" placeholder="data-volume" error={!!errors.name} {...register('name')} />
          {errors.name && <p className="text-sm text-red-500">{errors.name?.message as string}</p>}
        </div>

        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={onClose}>Отмена</Button>
          <Button type="submit" isLoading={mutation.isPending}>Создать</Button>
        </div>
      </form>
    </Modal>
  );
}