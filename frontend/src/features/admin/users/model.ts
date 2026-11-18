import { labelFor, type TFunction, type TranslationKey } from '@/shared/i18n';
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

export function userStatusLabel(t: TFunction, status?: string) {
  return labelFor(t, statusKeys, status);
}
