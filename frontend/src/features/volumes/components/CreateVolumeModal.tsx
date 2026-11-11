import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';

import { Modal } from '@/components/ui/Modal';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { type CreateVolumeForm, createVolumeSchema, type CreateVolumeDTO, MIB } from '../types';
import { useT } from '@/lib/i18n';
import { useCreateVolume } from '../hooks';

interface CreateVolumeModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function CreateVolumeModal({ isOpen, onClose }: CreateVolumeModalProps) {
  const t = useT();

  const { register, handleSubmit, reset, formState: { errors } } = useForm<CreateVolumeForm>({
    resolver: zodResolver(createVolumeSchema(t)),
  });

  const mutation = useCreateVolume();

  const onSubmit = (data: CreateVolumeForm) => {
    const sizeMiB = data.size_mib?.trim() ? Number(data.size_mib.trim()) : undefined;
    const dto: CreateVolumeDTO = {
      name: data.name,
      ...(sizeMiB ? { size_bytes: sizeMiB * MIB } : {}),
    };
    mutation.mutate(dto, {
      onSuccess: () => {
        reset();
        onClose();
      },
    });
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={t('volumes.create')} className="max-w-lg">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        <div className="space-y-2">
          <Label htmlFor="name">{t('volumes.name')}</Label>
          <Input id="name" placeholder="data-volume" error={!!errors.name} {...register('name')} />
          {errors.name && <p className="text-sm text-red-500">{errors.name?.message as string}</p>}
        </div>

        <div className="space-y-2">
          <Label htmlFor="size_mib">{t('volumes.size')}</Label>
          <Input
            id="size_mib"
            type="number"
            min={1}
            step={1}
            placeholder={t('volumes.sizeDefault')}
            error={!!errors.size_mib}
            {...register('size_mib')}
          />
          <p className="text-xs text-slate-500 dark:text-slate-400">{t('volumes.sizeHint')}</p>
          {errors.size_mib && <p className="text-sm text-red-500">{errors.size_mib?.message as string}</p>}
        </div>

        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={onClose}>{t('common.cancel')}</Button>
          <Button type="submit" isLoading={mutation.isPending}>{t('common.create')}</Button>
        </div>
      </form>
    </Modal>
  );
}
