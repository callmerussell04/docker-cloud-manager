import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

import { Modal } from '@/components/ui/Modal';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Button } from '@/components/ui/Button';
import { type ExposeContainerDTO, exposeContainerSchema, type ContainerData } from '../types';
import { BASE_DOMAIN } from '@/config';
import { useT } from '@/lib/i18n';
import { useExposeContainer } from '../hooks';

interface ExposeContainerModalProps {
  container: ContainerData | null;
  onClose: () => void;
}

type ExposeContainerFormValues = z.input<ReturnType<typeof exposeContainerSchema>>;

export function ExposeContainerModal({ container, onClose }: ExposeContainerModalProps) {
  const t = useT();

  const { register, handleSubmit, reset, formState: { errors } } = useForm<ExposeContainerFormValues>({
    resolver: zodResolver(exposeContainerSchema(t)),
  });

  useEffect(() => {
    if (container) {
      reset({
        domain_prefix: container.domain_prefix || '',
        internal_port: container.internal_port || 80,
      });
    }
  }, [container, reset]);

  const mutation = useExposeContainer();

  const onSubmit = (data: ExposeContainerFormValues) => {
    if (!container) return;
    mutation.mutate({
      id: container.id,
      data: {
        domain_prefix: data.domain_prefix,
        internal_port: Number(data.internal_port),
      },
    }, { onSuccess: onClose });
  };

  return (
    <Modal isOpen={!!container} onClose={onClose} title={t('containers.exposeTitle', { name: container?.name || '' })}>
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-4 mt-4">
        <div className="space-y-2">
          <Label htmlFor="domain_prefix">{t('containers.domainPrefix')}</Label>
          <div className="flex items-center gap-2">
            <Input
              id="domain_prefix"
              placeholder="my-app"
              error={!!errors.domain_prefix}
              {...register('domain_prefix')}
            />
            <span className="text-slate-500 dark:text-slate-400 whitespace-nowrap">.{BASE_DOMAIN}</span>
          </div>
          {errors.domain_prefix && <p className="text-sm text-red-500">{errors.domain_prefix.message}</p>}
        </div>

        <div className="space-y-2">
          <Label htmlFor="internal_port">{t('containers.internalContainerPort')}</Label>
          <Input
            id="internal_port"
            type="number"
            placeholder="80"
            error={!!errors.internal_port}
            {...register('internal_port')}
          />
          <p className="text-xs text-slate-500">{t('containers.internalPortHint')}</p>
          {errors.internal_port && <p className="text-sm text-red-500">{errors.internal_port.message}</p>}
        </div>

        <div className="bg-blue-50 dark:bg-blue-900/20 p-4 rounded-xl border border-blue-100 dark:border-blue-900/50 mt-4 text-sm text-blue-800 dark:text-blue-300">
          {t('containers.recreateNotice')}
        </div>

        <div className="flex justify-end gap-3 pt-6 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={onClose}>{t('common.cancel')}</Button>
          <Button type="submit" isLoading={mutation.isPending}>{t('common.save')}</Button>
        </div>
      </form>
    </Modal>
  );
}
