import { labelFor, type TFunction, type TranslationKey } from '@/shared/i18n';
export interface AdminUserAccount {
  user_id: string;
  username: string;
  email: string;
  role: 'admin' | 'user';
  status: 'active' | 'deactivated';
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
