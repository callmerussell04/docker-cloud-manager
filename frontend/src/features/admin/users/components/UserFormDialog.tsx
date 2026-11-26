import { useEffect, useState, type ReactNode } from 'react';
import { useForm, useWatch } from 'react-hook-form';
import { CircleCheck, CircleSlash, ShieldCheck, UserRound } from 'lucide-react';

import { useT } from '@/shared/i18n';
import { ChoiceCards } from '@/shared/ui/ChoiceCards';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { PasswordInput } from '@/shared/ui/PasswordInput';
import { StepDialog } from '@/shared/ui/StepDialog';
import { providerManagedFields, userRoleLabel, userStatusLabel, type AdminUser } from '../model';
import { useCreateAdminUser, useUpdateAdminUser } from '../queries';
import type { AdminUserForm } from '../schemas';
import { deactivatesOnSave, userStepIssues, userSteps } from '../userFlow';

interface UserFormDialogProps {
  isOpen: boolean;
  // user is the account to edit, or null to create one.
  user: AdminUser | null;
  onClose: () => void;
}

// UserFormDialog creates a user, or edits one, in three steps: Account (name,
// email and password), Access (role and status) and Review. Fields the
// identity provider owns are shown but not editable: the server refuses to
// change them, and a change would be undone at the next sign-in. Its state
// starts over each time it opens.
export function UserFormDialog({ isOpen, user, onClose }: UserFormDialogProps) {
  if (!isOpen) return null;
  return <UserFormFlow key={user?.user_id ?? 'new'} user={user} onClose={onClose} />;
}

function UserFormFlow({ user, onClose }: Omit<UserFormDialogProps, 'isOpen'>) {
  const t = useT();
  const managed = providerManagedFields(user);
  const [step, setStep] = useState(0);
  const [reached, setReached] = useState(user ? userSteps.length - 1 : 0);
  const { register, control, getValues, setValue, setError, clearErrors, setFocus, subscribe, formState: { errors } } = useForm<AdminUserForm>({
    defaultValues: user
      ? { username: user.username, email: user.email, password: '', role: user.role, status: user.status }
      : { username: '', email: '', password: '', role: 'user', status: 'active' },
  });
  const values = useWatch({ control }) as AdminUserForm;
  const createMutation = useCreateAdminUser();
  const updateMutation = useUpdateAdminUser();

  // A field's message goes once the field changes; Continue checks again.
  useEffect(() => subscribe({
    formState: { values: true },
    callback: ({ name }) => {
      if (name) clearErrors(name as keyof AdminUserForm);
    },
  }), [subscribe, clearErrors]);

  const showIssues = (index: number) => {
    const issues = userStepIssues(getValues(), userSteps[index], t, user);
    clearErrors();
    for (const issue of issues) setError(issue.field, { message: issue.message });
    if (issues.length > 0) setFocus(issues[0].field);
    return issues.length === 0;
  };

  const save = () => {
    const data = getValues();
    const payload: AdminUserForm = { ...data, password: data.password || undefined };
    if (user && managed.profile) Object.assign(payload, { username: user.username, email: user.email, password: undefined });
    if (user && managed.role) payload.role = user.role;
    if (user) updateMutation.mutate({ id: user.user_id, data: payload }, { onSuccess: onClose });
    else createMutation.mutate(payload, { onSuccess: onClose });
  };

  const submit = () => {
    if (userSteps[step] !== 'review') {
      if (showIssues(step)) {
        setStep(step + 1);
        setReached((furthest) => Math.max(furthest, step + 1));
      }
      return;
    }
    // A step that changed since it was checked is opened again with its
    // messages.
    const failing = userSteps.findIndex((target) => userStepIssues(getValues(), target, t, user).length > 0);
    if (failing >= 0) {
      setStep(failing);
      showIssues(failing);
      return;
    }
    save();
  };

  return (
    <StepDialog
      isOpen
      onClose={onClose}
      title={user ? t('admin.users.editTitle') : t('admin.users.createTitle')}
      steps={userSteps.map((id) => t(`admin.users.step.${id}`))}
      current={step}
      reached={reached}
      onStep={(index) => {
        clearErrors();
        setStep(index);
      }}
      onSubmit={submit}
      finishLabel={user ? t('common.save') : t('common.create')}
      isSubmitting={createMutation.isPending || updateMutation.isPending}
    >
      {userSteps[step] === 'account' && (
        <div className="grid gap-5">
          {managed.profile && <p className="text-xs leading-snug text-label-2">{t('admin.users.profileManagedHint')}</p>}
          <Field label={t('form.username')} htmlFor="user-username" error={errors.username?.message}>
            {managed.profile ? (
              <Input id="user-username" value={user?.username ?? ''} disabled readOnly />
            ) : (
              <Input id="user-username" autoComplete="off" spellCheck={false} error={!!errors.username} {...register('username')} />
            )}
          </Field>
          <Field label={t('form.email')} htmlFor="user-email" error={errors.email?.message}>
            {managed.profile ? (
              <Input id="user-email" type="email" value={user?.email ?? ''} disabled readOnly />
            ) : (
              <Input id="user-email" type="email" autoComplete="off" spellCheck={false} error={!!errors.email} {...register('email')} />
            )}
          </Field>
          <Field
            label={t('form.password')}
            htmlFor="user-password"
            hint={user && !managed.profile ? t('admin.users.passwordKeepHint') : undefined}
            error={errors.password?.message}
          >
            {managed.profile ? (
              <PasswordInput id="user-password" disabled />
            ) : (
              <PasswordInput id="user-password" autoComplete="new-password" error={!!errors.password} {...register('password')} />
            )}
          </Field>
        </div>
      )}
      {userSteps[step] === 'access' && (
        <div className="grid gap-6">
          <div className="grid gap-2">
            <span className="text-sm font-medium">{t('admin.users.role')}</span>
            {managed.role ? (
              <p className="text-sm">
                {userRoleLabel(t, values.role)}
                <span className="mt-1 block text-xs leading-snug text-label-2">{t('admin.users.roleManagedHint')}</span>
              </p>
            ) : (
              <ChoiceCards
                label={t('admin.users.role')}
                value={values.role}
                onChange={(role) => setValue('role', role)}
                items={[
                  { id: 'user', icon: UserRound, title: t('admin.users.roleUser'), description: t('admin.users.roleUserHint') },
                  { id: 'admin', icon: ShieldCheck, title: t('admin.users.roleAdmin'), description: t('admin.users.roleAdminHint') },
                ]}
              />
            )}
          </div>
          <div className="grid gap-2">
            <span className="text-sm font-medium">{t('common.status')}</span>
            <ChoiceCards
              label={t('common.status')}
              value={values.status}
              onChange={(status) => setValue('status', status)}
              items={[
                { id: 'active', icon: CircleCheck, title: t('status.active'), description: t('admin.users.statusActiveHint') },
                { id: 'deactivated', icon: CircleSlash, title: t('status.deactivated'), description: t('admin.users.statusDeactivatedHint') },
              ]}
            />
          </div>
        </div>
      )}
      {userSteps[step] === 'review' && <ReviewStep user={user} values={values} />}
    </StepDialog>
  );
}

function ReviewStep({ user, values }: { user: AdminUser | null; values: AdminUserForm }) {
  const t = useT();
  const managed = providerManagedFields(user);
  const name = managed.profile && user ? user.username : values.username.trim();
  const password = managed.profile
    ? t('admin.users.passwordManaged')
    : values.password ? t('admin.users.passwordSet') : t('admin.users.passwordKept');
  const facts: [string, ReactNode][] = [
    [t('form.username'), <span className="break-all">{name}</span>],
    [t('form.email'), <span className="break-all">{managed.profile && user ? user.email : values.email.trim()}</span>],
    [t('form.password'), password],
    [t('admin.users.role'), userRoleLabel(t, values.role)],
    [t('common.status'), userStatusLabel(t, values.status)],
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
      {deactivatesOnSave(user, values) && (
        <p className="text-xs leading-snug text-red-text">{t('admin.users.reviewDeactivates', { name })}</p>
      )}
    </div>
  );
}
