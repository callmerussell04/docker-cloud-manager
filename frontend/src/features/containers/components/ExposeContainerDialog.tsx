import { useEffect, useId } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

import { Dialog, DialogFooter, DialogHeader } from '@/shared/ui/Dialog';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { Button } from '@/shared/ui/Button';
import { exposeContainerSchema } from '../schemas';
import type { ContainerData } from '../model';
import { BASE_DOMAIN } from '@/shared/lib/config';
import { useT } from '@/shared/i18n';
import { useExposeContainer } from '../queries';

interface ExposeContainerDialogProps {
  // container is the one whose routing is edited; null keeps the dialog closed.
  container: ContainerData | null;
  onClose: () => void;
}

type ExposeContainerFormValues = z.input<ReturnType<typeof exposeContainerSchema>>;

// ExposeContainerDialog edits a container's routing (its subdomain and the
// port it opens) in one glass dialog, with the stepped dialogs' header and
// footer: title and close, the fields, then Cancel and Save.
export function ExposeContainerDialog({ container, onClose }: ExposeContainerDialogProps) {
  const t = useT();
  const titleId = useId();

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

  const title = t('containers.exposeTitle', { name: container?.name || '' });

  return (
    <Dialog isOpen={!!container} onClose={onClose} label={title} className="max-w-[480px] rounded-3xl">
      <DialogHeader title={title} titleId={titleId} onClose={onClose} />
      <form noValidate aria-labelledby={titleId} onSubmit={handleSubmit(onSubmit)} className="flex min-h-0 flex-1 flex-col">
        <div className="grid flex-1 gap-5 overflow-y-auto px-6 pb-6 pt-4">
          <Field label={t('containers.domainPrefix')} htmlFor="domain_prefix" error={errors.domain_prefix?.message as string | undefined}>
            <Input
              id="domain_prefix"
              data-autofocus
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
              inputMode="numeric"
              placeholder="80"
              error={!!errors.internal_port}
              {...register('internal_port')}
            />
          </Field>
          <p className="text-xs leading-snug text-label-2">{t('containers.recreateNotice')}</p>
        </div>
        <DialogFooter>
          <div className="flex-1" />
          <Button type="button" variant="secondary" className="rounded-full" onClick={onClose}>{t('common.cancel')}</Button>
          <Button type="submit" className="rounded-full" isLoading={mutation.isPending}>{t('common.save')}</Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
}
