import { useEffect, useState } from 'react';
import { useForm, useWatch } from 'react-hook-form';

import { useT } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { StepDialog } from '@/shared/ui/StepDialog';
import type { AdminUser } from '../model';
import { useResetAdminUserQuota, useSetAdminUserQuota } from '../queries';
import type { UserQuotaForm } from '../schemas';
import { quotaStepIssues, quotaSteps } from '../userFlow';

const MIB = 1024 * 1024;

interface UserQuotaDialogProps {
  // user is the account whose quota is edited; null keeps the dialog closed.
  user: AdminUser | null;
  onClose: () => void;
}

// UserQuotaDialog sets a user's quota in two steps, Quota (CPU, memory and
// disk) and Review, through the quota routes; a custom quota can instead be
// reset to the default from the first step. Its state starts over each time
// it opens.
export function UserQuotaDialog({ user, onClose }: UserQuotaDialogProps) {
  if (!user) return null;
  return <UserQuotaFlow key={user.user_id} user={user} onClose={onClose} />;
}

function UserQuotaFlow({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const t = useT();
  const [step, setStep] = useState(0);
  const [reached, setReached] = useState(0);
  const setQuota = useSetAdminUserQuota();
  const resetQuota = useResetAdminUserQuota();
  const { register, control, getValues, setError, clearErrors, setFocus, subscribe, formState: { errors } } = useForm<UserQuotaForm>({
    defaultValues: { quota_cpu: user.quota_cpu, quota_ram_mb: user.quota_ram_mb, quota_disk_mb: user.quota_disk_mb },
  });
  const values = useWatch({ control }) as UserQuotaForm;

  // A field's message goes once the field changes; Continue checks again.
  useEffect(() => subscribe({
    formState: { values: true },
    callback: ({ name }) => {
      if (name) clearErrors(name as keyof UserQuotaForm);
    },
  }), [subscribe, clearErrors]);

  const submit = () => {
    const issues = quotaStepIssues(getValues(), 'quota', t);
    if (issues.length > 0) {
      setStep(0);
      clearErrors();
      for (const issue of issues) setError(issue.field, { message: issue.message });
      setFocus(issues[0].field);
      return;
    }
    if (quotaSteps[step] !== 'review') {
      setStep(step + 1);
      setReached((furthest) => Math.max(furthest, step + 1));
      return;
    }
    setQuota.mutate({ id: user.user_id, data: getValues() }, { onSuccess: onClose });
  };

  const number = { valueAsNumber: true } as const;

  return (
    <StepDialog
      isOpen
      onClose={onClose}
      title={t('admin.users.quotaTitle', { name: user.username })}
      steps={quotaSteps.map((id) => t(`admin.users.quotaStep.${id}`))}
      current={step}
      reached={reached}
      onStep={(index) => {
        clearErrors();
        setStep(index);
      }}
      onSubmit={submit}
      finishLabel={t('common.save')}
      isSubmitting={setQuota.isPending}
    >
      {quotaSteps[step] === 'quota' && (
        <div className="grid gap-5">
          <p className="text-xs leading-snug text-label-2">
            {user.quota_custom ? t('admin.users.quotaIsCustom') : t('admin.users.quotaIsDefault')}
            {user.quota_custom && (
              <>
                {' '}
                <button
                  type="button"
                  disabled={resetQuota.isPending}
                  onClick={() => resetQuota.mutate(user.user_id, { onSuccess: onClose })}
                  className="font-semibold text-accent hover:underline disabled:opacity-50"
                >
                  {t('admin.users.resetQuota')}
                </button>
              </>
            )}
          </p>
          <Field label={t('admin.users.cpuQuota')} htmlFor="quota-cpu" error={errors.quota_cpu?.message}>
            <Input id="quota-cpu" type="number" step="0.001" inputMode="decimal" suffix={t('admin.users.cores')} error={!!errors.quota_cpu} {...register('quota_cpu', number)} />
          </Field>
          <Field label={t('admin.users.ramQuota')} htmlFor="quota-ram" error={errors.quota_ram_mb?.message}>
            <Input id="quota-ram" type="number" inputMode="numeric" suffix={t('admin.users.megabytes')} error={!!errors.quota_ram_mb} {...register('quota_ram_mb', number)} />
          </Field>
          <Field label={t('admin.users.diskQuota')} htmlFor="quota-disk" error={errors.quota_disk_mb?.message}>
            <Input id="quota-disk" type="number" inputMode="numeric" suffix={t('admin.users.megabytes')} error={!!errors.quota_disk_mb} {...register('quota_disk_mb', number)} />
          </Field>
        </div>
      )}
      {quotaSteps[step] === 'review' && (
        <div className="grid gap-5">
          <dl className="grid gap-4">
            {([
              [t('admin.users.cpuQuota'), `${values.quota_cpu} ${t('admin.users.cores')}`],
              [t('admin.users.ramQuota'), formatBytes(values.quota_ram_mb * MIB, 1)],
              [t('admin.users.diskQuota'), formatBytes(values.quota_disk_mb * MIB, 1)],
            ] as const).map(([label, value]) => (
              <div key={label} className="grid gap-0.5">
                <dt className="text-xs font-medium text-label-3">{label}</dt>
                <dd className="tabular-nums">{value}</dd>
              </div>
            ))}
          </dl>
          <p className="text-xs leading-snug text-label-2">{t('admin.users.quotaHint')}</p>
        </div>
      )}
    </StepDialog>
  );
}
