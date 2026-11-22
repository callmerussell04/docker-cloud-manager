import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';

import { Modal } from '@/shared/ui/Modal';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { Button } from '@/shared/ui/Button';
import { type CreateVolumeForm, createVolumeSchema } from '../schemas';
import { type CreateVolumeDTO, MIB } from '../model';
import { useT } from '@/shared/i18n';
import { useCreateVolume } from '../queries';

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
        <Field label={t('volumes.name')} htmlFor="name" error={errors.name?.message as string | undefined}>
          <Input id="name" placeholder="data-volume" error={!!errors.name} {...register('name')} />
        </Field>

        <Field label={t('volumes.size')} htmlFor="size_mib" hint={t('volumes.sizeHint')} error={errors.size_mib?.message as string | undefined}>
          <Input
            id="size_mib"
            type="number"
            min={1}
            step={1}
            placeholder={t('volumes.sizeDefault')}
            error={!!errors.size_mib}
            {...register('size_mib')}
          />
        </Field>

        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={onClose}>{t('common.cancel')}</Button>
          <Button type="submit" isLoading={mutation.isPending}>{t('common.create')}</Button>
        </div>
      </form>
    </Modal>
  );
}
