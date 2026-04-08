import { useForm, useFieldArray } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, Trash2 } from 'lucide-react';

import { Modal } from '@/components/ui/Modal';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { Select } from '@/components/ui/Select';
import { useToastStore } from '@/store/toastStore';
import { getImagesFn } from '@/features/images/api';
import { getVolumesFn } from '@/features/volumes/api';
import { createContainerFn } from '../api';
import { type CreateContainerForm, createContainerSchema, type CreateContainerDTO } from '../types';

interface CreateContainerModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export function CreateContainerModal({ isOpen, onClose }: CreateContainerModalProps) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const { data: images = [] } = useQuery({
    queryKey: ['images'],
    queryFn: getImagesFn,
    enabled: isOpen,
  });

  const { data: volumes = [] } = useQuery({
    queryKey: ['volumes'],
    queryFn: getVolumesFn,
    enabled: isOpen,
  });

  const { register, control, handleSubmit, reset, formState: { errors } } = useForm<CreateContainerForm>({
    resolver: zodResolver(createContainerSchema),
    defaultValues: {
      env_vars: [],
      volume_mounts: [],
    }
  });

  const { fields: envFields, append: appendEnv, remove: removeEnv } = useFieldArray({
    control,
    name: "env_vars"
  });

  const { fields: volFields, append: appendVol, remove: removeVol } = useFieldArray({
    control,
    name: "volume_mounts"
  });

  const mutation = useMutation({
    mutationFn: createContainerFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['containers'] });
      addToast('Контейнер успешно создан', 'success');
      reset();
      onClose();
    },
    onError: () => {
      addToast('Ошибка при создании контейнера', 'error');
    },
  });

  const onSubmit = (data: CreateContainerForm) => {
    const dto: CreateContainerDTO = {
      name: data.name,
      image_tag: data.image_tag,
      internal_port: data.internal_port ? Number(data.internal_port) : undefined,
      domain_prefix: data.domain_prefix || undefined,
    };

    if (data.env_vars && data.env_vars.length > 0) {
      dto.env_vars = data.env_vars.reduce((acc, curr) => {
        if (curr.key) acc[curr.key] = curr.value || '';
        return acc;
      }, {} as Record<string, string>);
    }

    if (data.volume_mounts && data.volume_mounts.length > 0) {
      dto.volume_mounts = data.volume_mounts.map(v => ({
        volume_id: v.volume_id,
        mount_path: v.mount_path,
        is_readonly: v.is_readonly
      }));
    }

    mutation.mutate(dto);
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Создать контейнер" className="max-w-2xl max-h-[90vh] overflow-y-auto">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="name">Имя контейнера</Label>
            <Input id="name" placeholder="my-app" error={!!errors.name} {...register('name')} />
            {errors.name && <p className="text-sm text-red-500">{errors.name.message}</p>}
          </div>

          <div className="space-y-2">
            <Label htmlFor="image_tag">Образ</Label>
            <Select id="image_tag" error={!!errors.image_tag} {...register('image_tag')}>
              <option value="">Выберите образ</option>
              {images.map(img => (
                <option key={img.id} value={img.tag}>{img.tag}</option>
              ))}
            </Select>
            {errors.image_tag && <p className="text-sm text-red-500">{errors.image_tag.message}</p>}
          </div>
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">Переменные окружения</h4>
          {envFields.map((field, index) => (
            <div key={field.id} className="flex gap-2 items-start">
              <div className="flex-1">
                <Input placeholder="Ключ (например, PORT)" {...register(`env_vars.${index}.key`)} />
                {errors.env_vars?.[index]?.key && <p className="text-xs text-red-500 mt-1">{errors.env_vars[index]?.key?.message}</p>}
              </div>
              <div className="flex-1">
                <Input placeholder="Значение" {...register(`env_vars.${index}.value`)} />
              </div>
              <Button type="button" variant="danger" onClick={() => removeEnv(index)} className="px-3">
                <Trash2 className="w-4 h-4" />
              </Button>
            </div>
          ))}
          <Button type="button" variant="secondary" onClick={() => appendEnv({ key: '', value: '' })} className="w-full text-sm">
            <Plus className="w-4 h-4 mr-2" /> Добавить переменную
          </Button>
        </div>

        <div className="p-4 rounded-xl bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700/50 space-y-4">
          <h4 className="font-medium">Тома (Volumes)</h4>
          {volFields.map((field, index) => (
            <div key={field.id} className="flex gap-2 items-start flex-wrap md:flex-nowrap">
              <div className="flex-1 min-w-[200px]">
                <Select {...register(`volume_mounts.${index}.volume_id`)}>
                  <option value="">Выберите том</option>
                  {volumes.map(vol => (
                    <option key={vol.id} value={vol.id}>{vol.docker_name}</option>
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
              <Button type="button" variant="danger" onClick={() => removeVol(index)} className="px-3 h-10">
                <Trash2 className="w-4 h-4" />
              </Button>
            </div>
          ))}
          <Button type="button" variant="secondary" onClick={() => appendVol({ volume_id: '', mount_path: '', is_readonly: false })} className="w-full text-sm">
            <Plus className="w-4 h-4 mr-2" /> Примонтировать том
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