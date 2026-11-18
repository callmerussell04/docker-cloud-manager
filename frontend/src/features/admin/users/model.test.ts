import { expect, test } from 'vitest';
import { type AdminUserAccount, providerManagedFields } from './model';

const account = (auth_source: AdminUserAccount['auth_source'], role_managed_by_provider: boolean): AdminUserAccount => ({
  user_id: 'id', username: 'alice', email: 'alice@example.com', role: 'user', status: 'active', auth_source, role_managed_by_provider,
});

test.each([
  ['nothing for a new user', null, { profile: false, role: false }],
  ['nothing for a local user', account('local', false), { profile: false, role: false }],
  ['the profile of an OIDC user whose role DCM manages', account('oidc', false), { profile: true, role: false }],
  ['profile and role of an OIDC user whose role the provider manages', account('oidc', true), { profile: true, role: true }],
] as const)('providerManagedFields locks %s', (_, user, expected) => {
  expect(providerManagedFields(user)).toEqual(expected);
});
