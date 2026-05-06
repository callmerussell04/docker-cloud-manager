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
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';

interface CreateVolumeModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function CreateVolumeModal({ isOpen, onClose }: CreateVolumeModalProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  const { register, handleSubmit, reset, formState: { errors } } = useForm<CreateVolumeForm>({
    resolver: zodResolver(createVolumeSchema(t)),
  });

  const mutation = useMutation({
    mutationFn: createVolumeFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['volumes'] });
      addToast(t('volumes.created'), 'success');
      reset();
      onClose();
    },
    onError: (error: unknown) => {
      const { message, requestId } = getApiErrorMessage(error, t('volumes.createFailed'), t);
      addToast(message, 'error', { requestId });
    },
  });

  const onSubmit = (data: CreateVolumeForm) => {
    const dto: CreateVolumeDTO = {
      name: data.name,
    };
    mutation.mutate(dto);
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={t('volumes.create')} className="max-w-lg">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        <div className="space-y-2">
          <Label htmlFor="name">{t('volumes.name')}</Label>
          <Input id="name" placeholder="data-volume" error={!!errors.name} {...register('name')} />
          {errors.name && <p className="text-sm text-red-500">{errors.name?.message as string}</p>}
        </div>

        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={onClose}>{t('common.cancel')}</Button>
          <Button type="submit" isLoading={mutation.isPending}>{t('common.create')}</Button>
        </div>
      </form>
    </Modal>
  );
}
