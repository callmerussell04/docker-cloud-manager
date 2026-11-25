import { describe, expect, test } from 'vitest';
import { type AdminUser, type AdminUserAccount, defaultUserSort, matchesUserSearch, providerManagedFields, sortUsers, userTone } from './model';

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

const user = (overrides: Partial<AdminUser> = {}): AdminUser => ({
  user_id: overrides.username ?? 'id',
  username: 'alice',
  email: 'alice@example.com',
  role: 'user',
  status: 'active',
  auth_source: 'local',
  role_managed_by_provider: false,
  quota_cpu: 2,
  quota_ram_mb: 4096,
  quota_disk_mb: 10240,
  quota_custom: false,
  ...overrides,
});

test.each([
  ['active', 'ok'],
  ['deactivated', 'idle'],
] as const)('userTone(%s) -> %s', (status, tone) => {
  expect(userTone(status)).toBe(tone);
});

describe('sortUsers', () => {
  const names = (users: AdminUser[]) => users.map((entry) => entry.username);
  const users = [
    user({ username: 'carol', role: 'admin', status: 'deactivated', quota_custom: true, quota_ram_mb: 2048 }),
    user({ username: 'alice' }),
    user({ username: 'bob', role: 'admin', quota_custom: true, quota_ram_mb: 8192 }),
  ];

  test('starts by name, from A', () => {
    expect(names(sortUsers(users, defaultUserSort))).toEqual(['alice', 'bob', 'carol']);
  });

  test('puts admins first by role, names breaking the tie', () => {
    expect(names(sortUsers(users, { key: 'role', ascending: true }))).toEqual(['bob', 'carol', 'alice']);
  });

  test('puts active accounts first by status', () => {
    expect(names(sortUsers(users, { key: 'status', ascending: true }))).toEqual(['alice', 'bob', 'carol']);
  });

  test('ranks custom quotas first, the larger memory first among them', () => {
    expect(names(sortUsers(users, { key: 'quota', ascending: true }))).toEqual(['bob', 'carol', 'alice']);
    expect(names(sortUsers(users, { key: 'quota', ascending: false }))).toEqual(['alice', 'carol', 'bob']);
  });
});

test.each([
  ['', true],
  ['ALI', true],
  ['example.com', true],
  ['bob', false],
])('matchesUserSearch(%j) -> %s', (query, expected) => {
  expect(matchesUserSearch(user(), query)).toBe(expected);
});
