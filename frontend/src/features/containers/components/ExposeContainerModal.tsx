import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

import { Modal } from '@/shared/ui/Modal';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { Button } from '@/shared/ui/Button';
import { exposeContainerSchema } from '../schemas';
import type { ContainerData } from '../model';
import { BASE_DOMAIN } from '@/shared/lib/config';
import { useT } from '@/shared/i18n';
import { useExposeContainer } from '../queries';

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
        <Field label={t('containers.domainPrefix')} htmlFor="domain_prefix" error={errors.domain_prefix?.message as string | undefined}>
          <Input
            id="domain_prefix"
            placeholder="my-app"
            suffix={`.${BASE_DOMAIN}`}
            error={!!errors.domain_prefix}
            {...register('domain_prefix')}
          />
        </Field>

        <Field label={t('containers.internalContainerPort')} htmlFor="internal_port" hint={t('containers.internalPortHint')} error={errors.internal_port?.message as string | undefined}>
          <Input
            id="internal_port"
            type="number"
            placeholder="80"
            error={!!errors.internal_port}
            {...register('internal_port')}
          />
        </Field>

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
