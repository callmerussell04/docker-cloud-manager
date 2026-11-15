import { privateApi } from '@/shared/api/client';
import type { AdminUser, AdminUserAccount, AdminUserDeactivation, AdminUserUpdate, UserQuota } from './model';
import type { AdminUserForm, UserQuotaForm } from './schemas';

export const getAdminUsersFn = async (page: number, limit: number) => {
  const response = await privateApi.get<{ users: AdminUser[], total_count: number }>('/admin/users', {
    params: { page, limit },
  });
  return { items: response.data.users || [], total_count: response.data.total_count || 0 };
};

export const createAdminUserFn = async (data: AdminUserForm): Promise<AdminUserAccount> => {
  const response = await privateApi.post<AdminUserAccount>('/admin/users', data);
  return response.data;
};

/** Saves the account. Setting the status to deactivated also stops the user's workloads. */
export const updateAdminUserFn = async ({ id, data }: { id: string; data: AdminUserForm }): Promise<AdminUserUpdate> => {
  const response = await privateApi.put<AdminUserUpdate>(`/admin/users/${id}`, data);
  return response.data;
};

/** Deactivates the user and stops their workloads. Repeating it stops the workloads that remain. */
export const deactivateAdminUserFn = async (id: string): Promise<AdminUserDeactivation> => {
  const response = await privateApi.delete<AdminUserDeactivation>(`/admin/users/${id}`);
  return response.data;
};

export const reactivateAdminUserFn = async (id: string): Promise<AdminUserAccount> => {
  const response = await privateApi.post<AdminUserAccount>(`/admin/users/${id}/activate`);
  return response.data;
};

export const setAdminUserQuotaFn = async ({ id, data }: { id: string; data: UserQuotaForm }): Promise<UserQuota> => {
  const response = await privateApi.put<UserQuota>(`/admin/users/${id}/quota`, data);
  return response.data;
};

export const resetAdminUserQuotaFn = async (id: string): Promise<UserQuota> => {
  const response = await privateApi.delete<UserQuota>(`/admin/users/${id}/quota`);
  return response.data;
};
