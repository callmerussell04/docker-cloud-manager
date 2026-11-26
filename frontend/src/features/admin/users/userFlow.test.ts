import { describe, expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import type { AdminUser } from './model';
import type { AdminUserForm } from './schemas';
import { deactivatesOnSave, quotaStepIssues, userStepIssues } from './userFlow';

const t = ((key: string) => key) as TFunction;

const form = (overrides: Partial<AdminUserForm> = {}): AdminUserForm => ({
  username: 'alice', email: 'alice@example.com', password: '', role: 'user', status: 'active', ...overrides,
});

const existing = (overrides: Partial<AdminUser> = {}): AdminUser => ({
  user_id: 'id', username: 'alice', email: 'alice@example.com', role: 'user', status: 'active', auth_source: 'local',
  role_managed_by_provider: false, quota_cpu: 2, quota_ram_mb: 4096, quota_disk_mb: 10240, quota_custom: false, ...overrides,
});

const fields = (issues: { field: string }[]) => issues.map((issue) => issue.field);

describe('userStepIssues', () => {
  test('asks a new user for a name, a valid email and a password on the account step', () => {
    const issues = userStepIssues(form({ username: '', email: 'nope' }), 'account', t, null);
    expect(fields(issues)).toEqual(['username', 'email', 'password']);
    expect(issues.find((issue) => issue.field === 'password')?.message).toBe('validation.passwordRequired');
  });

  test('keeps an edited user\'s password when it is left empty', () => {
    expect(userStepIssues(form(), 'account', t, existing())).toEqual([]);
  });

  test('does not check the fields the identity provider owns', () => {
    const oidc = existing({ auth_source: 'oidc' });
    expect(userStepIssues(form({ username: '', email: '' }), 'account', t, oidc)).toEqual([]);
  });

  test('checks nothing on the account step\'s behalf on the later steps', () => {
    expect(userStepIssues(form({ username: '' }), 'access', t, null)).toEqual([]);
    expect(userStepIssues(form({ username: '' }), 'review', t, null)).toEqual([]);
  });
});

test.each([
  ['an active user set to deactivated', existing(), 'deactivated', true],
  ['a deactivated user left deactivated', existing({ status: 'deactivated' }), 'deactivated', false],
  ['an active user left active', existing(), 'active', false],
  ['a new user created deactivated', null, 'deactivated', false],
] as const)('deactivatesOnSave: %s -> %s', (_, user, status, expected) => {
  expect(deactivatesOnSave(user, form({ status }))).toBe(expected);
});

describe('quotaStepIssues', () => {
  test('refuses an empty or non-positive figure on the quota step', () => {
    const issues = quotaStepIssues({ quota_cpu: 0, quota_ram_mb: Number.NaN, quota_disk_mb: 1024 }, 'quota', t);
    expect(fields(issues)).toEqual(['quota_cpu', 'quota_ram_mb']);
    expect(issues.every((issue) => issue.message === 'validation.quotaPositive')).toBe(true);
  });

  test('accepts positive figures, fractions of a core included', () => {
    expect(quotaStepIssues({ quota_cpu: 0.5, quota_ram_mb: 512, quota_disk_mb: 1024 }, 'quota', t)).toEqual([]);
  });
});
