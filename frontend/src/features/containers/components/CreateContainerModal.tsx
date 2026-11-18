import { useForm, useFieldArray } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { Plus, Trash2 } from 'lucide-react';

import { Modal } from '@/shared/ui/Modal';
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
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="name">{t('containers.containerName')}</Label>
            <Input id="name" placeholder="my-app" error={!!errors.name} {...register('name')} />
            {errors.name && <p className="text-sm text-red-500">{errors.name?.message as string}</p>}
          </div>

          <div className="space-y-2">
            <Label htmlFor="image_tag">{t('containers.image')}</Label>
            <Input 
              id="image_tag" 
              list="image-suggestions" 
              placeholder="nginx:latest" 
              error={!!errors.image_tag} 
              {...register('image_tag')} 
            />
            <datalist id="image-suggestions">
              {images.map(img => (
                <option key={img.id} value={img.tag} />
              ))}
            </datalist>
            <p className="text-xs text-slate-500">{t('containers.imageHint')}</p>
            {errors.image_tag && <p className="text-sm text-red-500">{errors.image_tag?.message as string}</p>}
          </div>
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">{t('containers.routingOptional')}</h4>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="domain_prefix">{t('containers.domainPrefix')}</Label>
              <div className="flex items-center gap-2">
                <Input id="domain_prefix" placeholder="my-app" error={!!errors.domain_prefix} {...register('domain_prefix')} />
                <span className="text-slate-500 dark:text-slate-400 whitespace-nowrap">.{BASE_DOMAIN}</span>
              </div>
              {errors.domain_prefix && <p className="text-sm text-red-500">{errors.domain_prefix?.message as string}</p>}
            </div>

            <div className="space-y-2">
              <Label htmlFor="internal_port">{t('containers.internalPort')}</Label>
              <Input id="internal_port" type="number" placeholder="80" error={!!errors.internal_port} {...register('internal_port')} />
              {errors.internal_port && <p className="text-sm text-red-500">{errors.internal_port?.message as string}</p>}
            </div>
          </div>
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">{t('containers.reservationOptional')}</h4>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="memory_reservation_mb">{t('containers.memoryReservation')}</Label>
              <Input id="memory_reservation_mb" type="number" min={16} step={1} placeholder={t('containers.reservationDefault')} error={!!errors.memory_reservation_mb} {...register('memory_reservation_mb')} />
              {errors.memory_reservation_mb && <p className="text-sm text-red-500">{errors.memory_reservation_mb?.message as string}</p>}
            </div>

            <div className="space-y-2">
              <Label htmlFor="cpu_reservation_cores">{t('containers.cpuReservation')}</Label>
              <Input id="cpu_reservation_cores" type="number" min={0.01} step="any" placeholder={t('containers.reservationDefault')} error={!!errors.cpu_reservation_cores} {...register('cpu_reservation_cores')} />
              {errors.cpu_reservation_cores && <p className="text-sm text-red-500">{errors.cpu_reservation_cores?.message as string}</p>}
            </div>
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
                <Select {...register(`volume_mounts.${index}.volume_id`)}>
                  <option value="">{t('containers.selectVolume')}</option>
                  {volumes.map(vol => (
                    <option key={vol.id} value={vol.id}>{vol.name || `volume-${vol.id.slice(0, 8)}`}</option>
                  ))}
                </Select>
                {errors.volume_mounts?.[index]?.volume_id && <p className="text-xs text-red-500 mt-1">{errors.volume_mounts[index]?.volume_id?.message}</p>}
              </div>
              <div className="flex-1 min-w-[150px]">
                <Input placeholder="/app/data" {...register(`volume_mounts.${index}.mount_path`)} />
                {errors.volume_mounts?.[index]?.mount_path && <p className="text-xs text-red-500 mt-1">{errors.volume_mounts[index]?.mount_path?.message}</p>}
              </div>
              <div className="flex items-center gap-2 h-10 px-2">
                <input type="checkbox" id={`ro-${index}`} {...register(`volume_mounts.${index}.is_readonly`)} className="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500" />
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
