import { labelFor, type TFunction, type TranslationKey } from '@/shared/i18n';
import { sortRows, type ListSort } from '@/shared/lib/listState';
import { matchesSearch } from '@/shared/lib/names';
import type { StatusTone } from '@/shared/ui/statusTone';

export interface AdminUserAccount {
  user_id: string;
  username: string;
  email: string;
  role: 'admin' | 'user';
  status: 'active' | 'deactivated';
  /** Where the account comes from; the identity provider owns an OIDC user's username, email and password. */
  auth_source: 'local' | 'oidc';
  /** True when the identity provider owns the role, so DCM refuses to change it. */
  role_managed_by_provider: boolean;
}

/** Which fields of the edit form the identity provider owns, so the form disables them. A new user has none. */
export function providerManagedFields(user: AdminUserAccount | null) {
  return {
    profile: user?.auth_source === 'oidc',
    role: !!user?.role_managed_by_provider,
  };
}

/** A user's effective quota from Core. quota_custom is false for the default quota. */
export interface UserQuota {
  quota_cpu: number;
  quota_ram_mb: number;
  quota_disk_mb: number;
  quota_custom: boolean;
}

export interface AdminUser extends AdminUserAccount, UserQuota {}

/** What deactivation did to the user's workloads. remaining counts those not yet stopped or canceled. */
export interface WorkloadSuspension {
  stopped_containers: number;
  canceled_builds: number;
  canceled_deployments: number;
  remaining: number;
}

export interface AdminUserDeactivation extends AdminUserAccount {
  workloads: WorkloadSuspension;
}

/** An updated user. workloads is present only when the update left the account deactivated. */
export interface AdminUserUpdate extends AdminUserAccount {
  workloads?: WorkloadSuspension;
}

const statusKeys: Record<string, TranslationKey> = {
  active: 'status.active',
  deactivated: 'status.deactivated',
};

// userRoleLabel names an account's role.
export function userRoleLabel(t: TFunction, role: AdminUserAccount['role']) {
  return role === 'admin' ? t('admin.users.roleAdmin') : t('admin.users.roleUser');
}

export function userStatusLabel(t: TFunction, status?: string) {
  return labelFor(t, statusKeys, status);
}

// userTone is an account's status dot: active is ok, deactivated at rest.
export function userTone(status: AdminUserAccount['status']): StatusTone {
  return status === 'active' ? 'ok' : 'idle';
}

export type UserSortKey = 'name' | 'role' | 'status' | 'quota';
export const userSortKeys: readonly UserSortKey[] = ['name', 'role', 'status', 'quota'];

// The users list starts by name; no account is failing.
export const defaultUserSort: ListSort<UserSortKey> = { key: 'name', ascending: true };

function userSortValue(user: AdminUser, key: UserSortKey): string | number {
  switch (key) {
    case 'name':
      return user.username;
    case 'role':
      return user.role === 'admin' ? 0 : 1;
    case 'status':
      return user.status === 'active' ? 0 : 1;
    case 'quota':
      // Custom quotas first, the larger memory quota first among each kind.
      return (user.quota_custom ? 0 : 1) * 1e12 - user.quota_ram_mb;
  }
}

// sortUsers orders the users list by one column. Ascending role is admins
// first, status active first, and quota custom first with the larger memory
// first. Users that tie keep name order, whichever way the column sorts.
export function sortUsers(users: readonly AdminUser[], sort: ListSort<UserSortKey>): AdminUser[] {
  return sortRows(users, (user) => [userSortValue(user, sort.key), user.username, user.user_id], sort.ascending);
}

// matchesUserSearch finds a user by username or email, without case.
export function matchesUserSearch(user: AdminUserAccount, search: string) {
  return matchesSearch(search, [user.username, user.email]);
}
