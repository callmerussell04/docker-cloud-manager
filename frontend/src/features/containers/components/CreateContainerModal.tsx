import { useForm, useFieldArray } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { Plus, Trash2 } from 'lucide-react';

import { Modal } from '@/shared/ui/Modal';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { Label } from '@/shared/ui/Label';
import { Button } from '@/shared/ui/Button';
import { Select } from '@/shared/ui/Select';
import { type CreateContainerForm, type CreateContainerValues, createContainerSchema } from '../schemas';
import type { CreateContainerDTO } from '../model';
import { BASE_DOMAIN } from '@/shared/lib/config';
import { useT } from '@/shared/i18n';
import { useImages } from '@/features/images/queries';
import { useVolumes } from '@/features/volumes/queries';
import { useCreateContainer } from '../queries';
import { KeyValueFieldArray } from '@/shared/ui/KeyValueFieldArray';

interface CreateContainerModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function CreateContainerModal({ isOpen, onClose }: CreateContainerModalProps) {
  const t = useT();

  const { data: imagesData } = useImages('own', 1, 100, isOpen);
  const { data: volumesData } = useVolumes('own', 1, 100, isOpen);
  const images = isOpen ? imagesData?.items || [] : [];
  const volumes = isOpen ? volumesData?.items || [] : [];

  const { register, control, handleSubmit, reset, formState: { errors } } = useForm<CreateContainerForm, unknown, CreateContainerValues>({
    resolver: zodResolver(createContainerSchema(t)),
    defaultValues: {
      env_vars: [],
      volume_mounts: [],
      domain_prefix: '',
      internal_port: '',
      memory_reservation_mb: '',
      cpu_reservation_cores: '',
    }
  });

  const { fields: volFields, append: appendVol, remove: removeVol } = useFieldArray({
    control,
    name: "volume_mounts"
  });

  const mutation = useCreateContainer();

  const onSubmit = (data: CreateContainerValues) => {
    const dto: CreateContainerDTO = {
      name: data.name,
      image_tag: data.image_tag,
      internal_port: data.internal_port,
      domain_prefix: data.domain_prefix,
      memory_reservation_mb: data.memory_reservation_mb,
      cpu_reservation_millicores: data.cpu_reservation_millicores,
    };

    if (data.env_vars && data.env_vars.length > 0) {
      dto.env_vars = data.env_vars.reduce((acc: Record<string, string>, curr) => {
        if (curr.key) acc[curr.key] = curr.value || '';
        return acc;
      }, {});
    }

    if (data.volume_mounts && data.volume_mounts.length > 0) {
      dto.volume_mounts = data.volume_mounts.map((v) => ({
        volume_id: v.volume_id,
        mount_path: v.mount_path,
        is_readonly: v.is_readonly
      }));
    }

    mutation.mutate(dto, {
      onSuccess: () => {
        reset();
        onClose();
      },
    });
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={t('containers.createTitle')} className="max-w-3xl">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        <datalist id="image-suggestions">
          {images.map(img => (
            <option key={img.id} value={img.tag} />
          ))}
        </datalist>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <Field label={t('containers.containerName')} htmlFor="name" error={errors.name?.message as string | undefined}>
            <Input id="name" placeholder="my-app" error={!!errors.name} {...register('name')} />
          </Field>

          <Field label={t('containers.image')} htmlFor="image_tag" hint={t('containers.imageHint')} error={errors.image_tag?.message as string | undefined}>
            <Input 
              id="image_tag" 
              list="image-suggestions" 
              placeholder="nginx:latest" 
              error={!!errors.image_tag} 
              {...register('image_tag')} 
            />
          </Field>
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">{t('containers.routingOptional')}</h4>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Field label={t('containers.domainPrefix')} htmlFor="domain_prefix" error={errors.domain_prefix?.message as string | undefined}>
              <Input id="domain_prefix" placeholder="my-app" suffix={`.${BASE_DOMAIN}`} error={!!errors.domain_prefix} {...register('domain_prefix')} />
            </Field>

            <Field label={t('containers.internalPort')} htmlFor="internal_port" error={errors.internal_port?.message as string | undefined}>
              <Input id="internal_port" type="number" placeholder="80" error={!!errors.internal_port} {...register('internal_port')} />
            </Field>
          </div>
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">{t('containers.reservationOptional')}</h4>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Field label={t('containers.memoryReservation')} htmlFor="memory_reservation_mb" error={errors.memory_reservation_mb?.message as string | undefined}>
              <Input id="memory_reservation_mb" type="number" min={16} step={1} placeholder={t('containers.reservationDefault')} error={!!errors.memory_reservation_mb} {...register('memory_reservation_mb')} />
            </Field>

            <Field label={t('containers.cpuReservation')} htmlFor="cpu_reservation_cores" error={errors.cpu_reservation_cores?.message as string | undefined}>
              <Input id="cpu_reservation_cores" type="number" min={0.01} step="any" placeholder={t('containers.reservationDefault')} error={!!errors.cpu_reservation_cores} {...register('cpu_reservation_cores')} />
            </Field>
          </div>
          <p className="text-xs text-slate-500">{t('containers.reservationHint')}</p>
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">{t('containers.envVars')}</h4>
          <KeyValueFieldArray
            control={control}
            register={register}
            name="env_vars"
            keyPlaceholder={t('containers.envKeyPlaceholder')}
            valuePlaceholder={t('containers.envValuePlaceholder')}
            addLabel={t('containers.addEnv')}
          />
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">{t('containers.volumes')}</h4>
          {volFields.map((field, index) => (
            <div key={field.id} className="flex gap-2 items-start flex-wrap md:flex-nowrap">
              <div className="flex-1 min-w-[200px]">
                <Field error={errors.volume_mounts?.[index]?.volume_id?.message}>
                  <Select {...register(`volume_mounts.${index}.volume_id`)}>
                    <option value="">{t('containers.selectVolume')}</option>
                    {volumes.map(vol => (
                      <option key={vol.id} value={vol.id}>{vol.name || `volume-${vol.id.slice(0, 8)}`}</option>
                    ))}
                  </Select>
                </Field>
              </div>
              <div className="flex-1 min-w-[150px]">
                <Field error={errors.volume_mounts?.[index]?.mount_path?.message}>
                  <Input placeholder="/app/data" {...register(`volume_mounts.${index}.mount_path`)} />
                </Field>
              </div>
              <div className="flex items-center gap-2 h-10 px-2">
                <input type="checkbox" id={`ro-${index}`} {...register(`volume_mounts.${index}.is_readonly`)} className="rounded accent-accent-fill" />
                <Label htmlFor={`ro-${index}`} className="text-xs cursor-pointer">RO</Label>
              </div>
              <Button title={t('common.delete')} aria-label={t('common.delete')} type="button" variant="danger" onClick={() => removeVol(index)} className="px-3 h-10">
                <Trash2 className="w-4 h-4" />
              </Button>
            </div>
          ))}
          <Button type="button" variant="secondary" onClick={() => appendVol({ volume_id: '', mount_path: '', is_readonly: false })} className="w-full text-sm">
            <Plus className="w-4 h-4 mr-2" /> {t('containers.mountVolume')}
          </Button>
        </div>

        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={onClose}>{t('common.cancel')}</Button>
          <Button type="submit" isLoading={mutation.isPending}>{t('common.create')}</Button>
        </div>
      </form>
    </Modal>
  );
}
