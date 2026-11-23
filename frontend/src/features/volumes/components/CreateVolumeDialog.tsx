import { useEffect, useState, type ReactNode } from 'react';
import { useForm, useWatch } from 'react-hook-form';

import { useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { Meter } from '@/shared/ui/Meter';
import { StepDialog } from '@/shared/ui/StepDialog';
import { createVolumeSteps, diskQuotaFit, typedSizeBytes, volumeStepIssues, type DiskQuota } from '../createFlow';
import type { CreateVolumeDTO } from '../model';
import { useCreateVolume } from '../queries';
import type { CreateVolumeForm } from '../schemas';

interface CreateVolumeDialogProps {
  isOpen: boolean;
  onClose: () => void;
  // disk is the user's disk quota and what they already reserve, for the
  // quota bar; undefined while it is not known.
  disk?: DiskQuota;
}

// CreateVolumeDialog creates a volume in two steps: Volume (its name and
// size, against the disk quota) and Review. Its state starts over each time
// it opens.
export function CreateVolumeDialog({ isOpen, onClose, disk }: CreateVolumeDialogProps) {
  if (!isOpen) return null;
  return <CreateVolumeFlow onClose={onClose} disk={disk} />;
}

const emptyForm: CreateVolumeForm = { name: '', size_mib: '' };

function toDTO(values: CreateVolumeForm): CreateVolumeDTO {
  const sizeBytes = typedSizeBytes(values.size_mib);
  return { name: values.name.trim(), ...(sizeBytes ? { size_bytes: sizeBytes } : {}) };
}

function CreateVolumeFlow({ onClose, disk }: Omit<CreateVolumeDialogProps, 'isOpen'>) {
  const t = useT();
  const [step, setStep] = useState(0);
  const [reached, setReached] = useState(0);
  const { register, control, getValues, setError, clearErrors, setFocus, subscribe, formState: { errors } } = useForm<CreateVolumeForm>({
    defaultValues: emptyForm,
  });
  const values = useWatch({ control }) as CreateVolumeForm;
  const mutation = useCreateVolume();

  // A field's message goes once the field changes; Continue checks again.
  useEffect(() => subscribe({
    formState: { values: true },
    callback: ({ name }) => {
      if (name) clearErrors(name as keyof CreateVolumeForm);
    },
  }), [subscribe, clearErrors]);

  const showIssues = (index: number) => {
    const issues = volumeStepIssues(getValues(), createVolumeSteps[index], t, disk);
    clearErrors();
    for (const issue of issues) setError(issue.field, { message: issue.message });
    if (issues.length > 0) setFocus(issues[0].field);
    return issues.length === 0;
  };

  const submit = () => {
    if (createVolumeSteps[step] !== 'review') {
      if (showIssues(step)) {
        setStep(step + 1);
        setReached((furthest) => Math.max(furthest, step + 1));
      }
      return;
    }
    // A step that changed since it was checked is opened again with its
    // messages.
    const failing = createVolumeSteps.findIndex((target) => volumeStepIssues(getValues(), target, t, disk).length > 0);
    if (failing >= 0) {
      setStep(failing);
      showIssues(failing);
      return;
    }
    mutation.mutate(toDTO(getValues()), { onSuccess: onClose });
  };

  return (
    <StepDialog
      isOpen
      onClose={onClose}
      title={t('volumes.create')}
      steps={createVolumeSteps.map((id) => t(`volumes.new.step.${id}`))}
      current={step}
      reached={reached}
      onStep={(index) => {
        clearErrors();
        setStep(index);
      }}
      onSubmit={submit}
      finishLabel={t('common.create')}
      isSubmitting={mutation.isPending}
    >
      {createVolumeSteps[step] === 'volume' && (
        <div className="grid gap-5">
          <Field label={t('volumes.new.name')} htmlFor="volume-name" hint={t('volumes.new.nameHint')} error={errors.name?.message}>
            <Input id="volume-name" placeholder="pg-data" autoComplete="off" spellCheck={false} error={!!errors.name} {...register('name')} />
          </Field>
          <Field label={t('volumes.size')} htmlFor="volume-size" hint={t('volumes.sizeHint')} error={errors.size_mib?.message}>
            <Input id="volume-size" inputMode="numeric" placeholder={t('volumes.sizeDefault')} autoComplete="off" error={!!errors.size_mib} {...register('size_mib')} />
          </Field>
          {disk && <DiskQuotaBar disk={disk} sizeMiB={values.size_mib} fieldError={!!errors.size_mib} />}
        </div>
      )}
      {createVolumeSteps[step] === 'review' && <ReviewStep values={values} />}
    </StepDialog>
  );
}

// DiskQuotaBar shows what the user's images and volumes hold, what this
// volume adds and what stays free; the added part turns red when it would not
// fit, with a line saying so until Continue puts the message under the field.
function DiskQuotaBar({ disk, sizeMiB, fieldError }: { disk: DiskQuota; sizeMiB: string | undefined; fieldError: boolean }) {
  const t = useT();
  const fit = diskQuotaFit(disk, sizeMiB);
  const amount = (bytes: number) => (bytes > 0 ? formatBytes(bytes, 1) : '0 MB');
  const parts = [{ value: fit.heldBytes, label: t('volumes.new.quotaHeld', { amount: amount(fit.heldBytes) }), className: 'bg-label-3' }];
  if (fit.addBytes !== undefined) {
    parts.push({
      value: fit.addBytes,
      label: t('volumes.new.quotaAdded', { amount: amount(fit.addBytes) }),
      className: fit.fits ? 'bg-accent-fill' : 'bg-red',
    });
  }
  return (
    <div className="grid gap-1.5">
      <div className="flex items-baseline justify-between gap-3 text-xs">
        <span className="font-medium text-label-2">{t('volumes.new.diskQuota')}</span>
        <span className="tabular-nums text-label-3">{amount(disk.quotaBytes)}</span>
      </div>
      <Meter parts={parts} total={disk.quotaBytes} freeLabel={t('volumes.new.quotaFree', { amount: amount(fit.freeBytes) })} />
      {!fit.fits && !fieldError && fit.addBytes !== undefined && (
        <p className="text-xs leading-snug text-red-text">{t('volumes.new.quotaOver', { amount: amount(fit.addBytes), free: amount(fit.availableBytes) })}</p>
      )}
      {fit.addBytes === undefined && <p className="text-xs leading-snug text-label-3">{t('volumes.new.quotaDefault')}</p>}
    </div>
  );
}

function ReviewStep({ values }: { values: CreateVolumeForm }) {
  const t = useT();
  const sizeBytes = typedSizeBytes(values.size_mib);
  const facts: [string, ReactNode][] = [
    [t('volumes.new.name'), <span className="break-all">{values.name.trim()}</span>],
    [t('volumes.column.size'), sizeBytes ? formatBytes(sizeBytes, 1) : t('volumes.sizeDefault')],
  ];
  return (
    <div className="grid gap-5">
      <dl className="grid gap-4">
        {facts.map(([label, value]) => (
          <div key={label} className="grid gap-0.5">
            <dt className="text-xs font-medium text-label-3">{label}</dt>
            <dd className="min-w-0">{value}</dd>
          </div>
        ))}
      </dl>
      <p className="text-xs leading-snug text-label-2">{t('volumes.new.reviewNote')}</p>
    </div>
  );
}
