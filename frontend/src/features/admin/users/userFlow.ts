import type { TFunction } from '@/shared/i18n';
import { type AdminUserAccount, providerManagedFields } from './model';
import { adminUserSchema, userQuotaSchema, type AdminUserForm, type UserQuotaForm } from './schemas';

// The user dialog's steps, in order: the account (name, email, password),
// its access (role and status), then a review.
export const userSteps = ['account', 'access', 'review'] as const;
export type UserStep = (typeof userSteps)[number];

// The quota dialog's steps: the three figures, then a review.
export const quotaSteps = ['quota', 'review'] as const;
export type QuotaStep = (typeof quotaSteps)[number];

export interface StepIssue<F> {
  field: F;
  message: string;
}

const userStepFields: Record<UserStep, readonly (keyof AdminUserForm)[]> = {
  account: ['username', 'email', 'password'],
  access: ['role', 'status'],
  review: [],
};

// userStepIssues is what keeps a step of the user dialog from moving on: the
// schema's messages for the fields the step shows, and on the account step a
// new user's missing password. user is the account being edited, or null for
// a new one; fields its identity provider owns are not checked, since the
// form sends the account's own values for them.
export function userStepIssues(values: AdminUserForm, step: UserStep, t: TFunction, user: AdminUserAccount | null): StepIssue<keyof AdminUserForm>[] {
  const managed = providerManagedFields(user);
  const skipped = new Set<keyof AdminUserForm>([
    ...(managed.profile ? (['username', 'email', 'password'] as const) : []),
    ...(managed.role ? (['role'] as const) : []),
  ]);
  const fields = userStepFields[step].filter((field) => !skipped.has(field));
  const result = adminUserSchema(t).safeParse(values);
  const issues = result.success ? [] : result.error.issues
    .filter((issue) => (fields as readonly PropertyKey[]).includes(issue.path[0]))
    .map((issue) => ({ field: issue.path[0] as keyof AdminUserForm, message: issue.message }));
  if (fields.includes('password') && !user && !values.password) {
    issues.push({ field: 'password', message: t('validation.passwordRequired') });
  }
  return issues;
}

// deactivatesOnSave says saving the form deactivates an active account,
// which also stops its workloads, so the review says so.
export function deactivatesOnSave(user: AdminUserAccount | null, values: AdminUserForm) {
  return user?.status === 'active' && values.status === 'deactivated';
}

// quotaStepIssues is what keeps a step of the quota dialog from moving on:
// the schema's messages for its figures.
export function quotaStepIssues(values: UserQuotaForm, step: QuotaStep, t: TFunction): StepIssue<keyof UserQuotaForm>[] {
  if (step !== 'quota') return [];
  const result = userQuotaSchema(t).safeParse(values);
  return result.success ? [] : result.error.issues.map((issue) => ({ field: issue.path[0] as keyof UserQuotaForm, message: issue.message }));
}
