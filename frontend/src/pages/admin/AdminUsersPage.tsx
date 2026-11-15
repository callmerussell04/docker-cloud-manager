import { useEffect, useState } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { Edit, Gauge, Plus, PowerOff, RefreshCcw, RotateCcw, UserX, Users } from 'lucide-react';

import { Button } from '@/shared/ui/Button';
import { Input } from '@/shared/ui/Input';
import { Label } from '@/shared/ui/Label';
import { Modal } from '@/shared/ui/Modal';
import { Pagination } from '@/shared/ui/Pagination';
import { Select } from '@/shared/ui/Select';
import { Badge } from '@/shared/ui/Badge';
import { useToastStore } from '@/shared/ui/toastStore';
import { type AdminUser, type AdminUserForm, adminUserSchema, type UserQuotaForm, userQuotaSchema } from '@/features/admin/types';
import { tableLayouts } from '@/shared/ui/tableLayouts';
import { cn } from '@/shared/lib/utils';
import { statusLabel, useT } from '@/shared/i18n';
import {
  useAdminUsers,
  useCreateAdminUser,
  useDeactivateAdminUser,
  useReactivateAdminUser,
  useResetAdminUserQuota,
  useSetAdminUserQuota,
  useUpdateAdminUser,
} from '@/features/admin/hooks';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';

export function AdminUsersPage() {
  const [page, setPage] = useState(1);
  const [editingUser, setEditingUser] = useState<AdminUser | null>(null);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [deactivateUser, setDeactivateUser] = useState<AdminUser | null>(null);
  const [quotaUser, setQuotaUser] = useState<AdminUser | null>(null);
  const limit = 20;
  const t = useT();

  const { data, isFetching, refetch } = useAdminUsers(page, limit);
  const deactivateMutation = useDeactivateAdminUser();
  // Deactivating an already deactivated user stops the workloads that remain.
  const stopWorkloadsOnly = deactivateUser?.status === 'deactivated';
  const reactivateMutation = useReactivateAdminUser();

  const users = data?.items || [];

  return (
    <div className="space-y-6 flex flex-col h-full">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 shrink-0">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-red-600 dark:text-red-400 flex items-center gap-3">
            <Users className="w-8 h-8" />
            {t('admin.users.title')}
          </h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">{t('admin.users.subtitle')}</p>
        </div>
        <div className="flex items-center gap-3">
          <Button variant="secondary" onClick={() => refetch()} isLoading={isFetching} className="px-3">
            <RefreshCcw className="w-4 h-4" />
          </Button>
          <Button onClick={() => setIsCreateOpen(true)}>
            <Plus className="w-4 h-4 mr-2" />
            {t('admin.users.create')}
          </Button>
        </div>
      </div>

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col">
        <div className="overflow-x-auto flex-1">
          <div className={tableLayouts.adminUsers.minWidth}>
            <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", tableLayouts.adminUsers.grid)}>
              <div>{t('admin.users.username')}</div>
              <div>{t('admin.users.emailId')}</div>
              <div>{t('admin.users.role')}</div>
              <div>{t('common.status')}</div>
              <div>{t('admin.users.quotas')}</div>
              <div className="flex justify-end">{t('admin.users.management')}</div>
            </div>

            {isFetching ? (
              <div className="p-12 flex justify-center opacity-50"><RefreshCcw className="w-8 h-8 animate-spin" /></div>
            ) : users.length > 0 ? (
              users.map((user) => (
                <div key={user.user_id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", tableLayouts.adminUsers.grid)}>
                  <div className="font-medium truncate">{user.username}</div>
                  <div className="min-w-0">
                    <div className="truncate">{user.email}</div>
                    <div className="text-xs font-mono text-slate-500 truncate">{user.user_id}</div>
                  </div>
                  <div><Badge variant={user.role === 'admin' ? 'error' : 'default'}>{user.role}</Badge></div>
                  <div><Badge variant={user.status === 'active' ? 'success' : 'default'}>{statusLabel(t, user.status)}</Badge></div>
                  <div className="text-xs text-slate-500 space-y-1">
                    <Badge variant={user.quota_custom ? 'warning' : 'default'}>
                      {user.quota_custom ? t('admin.users.quotaCustom') : t('admin.users.quotaDefault')}
                    </Badge>
                    <div>CPU: {user.quota_cpu}</div>
                    <div>RAM: {user.quota_ram_mb} MB</div>
                    <div>Disk: {user.quota_disk_mb} MB</div>
                  </div>
                  <div className="flex gap-2 justify-end shrink-0">
                    <Button variant="secondary" className="h-8 px-2" title={t('admin.users.editQuota')} aria-label={t('admin.users.editQuota')} onClick={() => setQuotaUser(user)}>
                      <Gauge className="w-4 h-4" />
                    </Button>
                    <Button variant="secondary" className="h-8 px-2" onClick={() => setEditingUser(user)}>
                      <Edit className="w-4 h-4" />
                    </Button>
                    {user.status === 'active' ? (
                      <Button
                        variant="danger"
                        className="h-8 px-2"
                        disabled={deactivateMutation.isPending}
                        onClick={() => setDeactivateUser(user)}
                      >
                        <UserX className="w-4 h-4" />
                      </Button>
                    ) : (
                      <>
                        <Button
                          variant="danger"
                          className="h-8 px-2"
                          title={t('admin.users.stopWorkloads')}
                          aria-label={t('admin.users.stopWorkloads')}
                          disabled={deactivateMutation.isPending}
                          onClick={() => setDeactivateUser(user)}
                        >
                          <PowerOff className="w-4 h-4" />
                        </Button>
                        <Button variant="secondary" className="h-8 px-2" disabled={reactivateMutation.isPending} onClick={() => reactivateMutation.mutate(user.user_id)}>
                          <RotateCcw className="w-4 h-4" />
                        </Button>
                      </>
                    )}
                  </div>
                </div>
              ))
            ) : (
              <div className="p-12 text-center text-slate-500">{t('admin.users.notFound')}</div>
            )}
          </div>
        </div>
        {data && <Pagination currentPage={page} pageSize={limit} totalItems={data.total_count} onPageChange={setPage} />}
      </div>

      <AdminUserModal
        isOpen={isCreateOpen || !!editingUser}
        user={editingUser}
        onClose={() => {
          setIsCreateOpen(false);
          setEditingUser(null);
        }}
      />
      <UserQuotaModal user={quotaUser} onClose={() => setQuotaUser(null)} />
      <ConfirmDialog
        isOpen={!!deactivateUser}
        title={t('confirm.title')}
        message={
          stopWorkloadsOnly
            ? t('admin.users.stopWorkloadsConfirm', { name: deactivateUser?.username || '' })
            : t('admin.users.deactivateConfirm', { name: deactivateUser?.username || '' })
        }
        confirmLabel={stopWorkloadsOnly ? t('admin.users.stopWorkloads') : t('admin.users.deactivate')}
        isLoading={deactivateMutation.isPending}
        onCancel={() => setDeactivateUser(null)}
        onConfirm={() => {
          if (deactivateUser) {
            deactivateMutation.mutate(deactivateUser.user_id, { onSuccess: () => setDeactivateUser(null) });
          }
        }}
      />
    </div>
  );
}

function AdminUserModal({ isOpen, user, onClose }: { isOpen: boolean; user: AdminUser | null; onClose: () => void }) {
  const addToast = useToastStore((state) => state.addToast);
  const isEdit = !!user;
  const t = useT();

  const { register, handleSubmit, reset, formState: { errors } } = useForm<AdminUserForm>({
    resolver: zodResolver(adminUserSchema(t)),
    defaultValues: {
      role: 'user',
      status: 'active',
    },
  });

  useEffect(() => {
    if (user) {
      reset({ username: user.username, email: user.email, role: user.role, status: user.status, password: '' });
    } else {
      reset({
        username: '',
        email: '',
        password: '',
        role: 'user',
        status: 'active',
      });
    }
  }, [user, reset]);

  const createMutation = useCreateAdminUser();
  const updateMutation = useUpdateAdminUser();

  const onSubmit = (data: AdminUserForm) => {
    if (!isEdit && !data.password) {
      addToast(t('validation.passwordRequired'), 'error');
      return;
    }
    const payload = { ...data, password: data.password || undefined };
    if (isEdit && user) {
      updateMutation.mutate({ id: user.user_id, data: payload }, { onSuccess: onClose });
    } else {
      createMutation.mutate(payload, { onSuccess: onClose });
    }
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={isEdit ? t('admin.users.editTitle') : t('admin.users.createTitle')} className="max-w-2xl">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-5">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="username">{t('form.username')}</Label>
            <Input id="username" {...register('username')} error={!!errors.username} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="email">{t('form.email')}</Label>
            <Input id="email" type="email" {...register('email')} error={!!errors.email} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="password">{t('form.password')}</Label>
            <Input id="password" type="password" placeholder={isEdit ? t('form.passwordKeep') : ''} {...register('password')} error={!!errors.password} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="role">{t('admin.users.role')}</Label>
            <Select id="role" {...register('role')}>
              <option value="user">user</option>
              <option value="admin">admin</option>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="status">{t('common.status')}</Label>
            <Select id="status" {...register('status')}>
              <option value="active">{t('status.active')}</option>
              <option value="deactivated">{t('status.deactivated')}</option>
            </Select>
          </div>
        </div>
        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={onClose}>{t('common.cancel')}</Button>
          <Button type="submit" isLoading={createMutation.isPending || updateMutation.isPending}>{t('common.save')}</Button>
        </div>
      </form>
    </Modal>
  );
}

/** Edits a user's quota through the quota routes; reset returns them to the default. */
function UserQuotaModal({ user, onClose }: { user: AdminUser | null; onClose: () => void }) {
  const t = useT();
  const setQuota = useSetAdminUserQuota();
  const resetQuota = useResetAdminUserQuota();
  const { register, handleSubmit, reset, formState: { errors } } = useForm<UserQuotaForm>({
    resolver: zodResolver(userQuotaSchema(t)),
  });

  useEffect(() => {
    if (user) {
      reset({ quota_cpu: user.quota_cpu, quota_ram_mb: user.quota_ram_mb, quota_disk_mb: user.quota_disk_mb });
    }
  }, [user, reset]);

  const onSubmit = (data: UserQuotaForm) => {
    if (user) {
      setQuota.mutate({ id: user.user_id, data }, { onSuccess: onClose });
    }
  };
  const firstError = errors.quota_cpu?.message || errors.quota_ram_mb?.message || errors.quota_disk_mb?.message;

  return (
    <Modal isOpen={!!user} onClose={onClose} title={t('admin.users.quotaTitle', { name: user?.username || '' })} className="max-w-xl">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-5">
        <p className="text-sm text-slate-500 dark:text-slate-400">
          {user?.quota_custom ? t('admin.users.quotaIsCustom') : t('admin.users.quotaIsDefault')} {t('admin.users.quotaHint')}
        </p>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div className="space-y-2">
            <Label htmlFor="quota_cpu">{t('admin.users.cpuQuota')}</Label>
            <Input id="quota_cpu" type="number" step="0.001" {...register('quota_cpu', { valueAsNumber: true })} error={!!errors.quota_cpu} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="quota_ram_mb">{t('admin.users.ramQuotaMb')}</Label>
            <Input id="quota_ram_mb" type="number" {...register('quota_ram_mb', { valueAsNumber: true })} error={!!errors.quota_ram_mb} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="quota_disk_mb">{t('admin.users.diskQuotaMb')}</Label>
            <Input id="quota_disk_mb" type="number" {...register('quota_disk_mb', { valueAsNumber: true })} error={!!errors.quota_disk_mb} />
          </div>
        </div>
        {firstError && <p className="text-sm text-red-600 dark:text-red-400">{firstError}</p>}
        <div className="flex justify-between gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button
            type="button"
            variant="secondary"
            disabled={!user?.quota_custom}
            isLoading={resetQuota.isPending}
            onClick={() => user && resetQuota.mutate(user.user_id, { onSuccess: onClose })}
          >
            <RotateCcw className="w-4 h-4 mr-2" />
            {t('admin.users.resetQuota')}
          </Button>
          <div className="flex gap-3">
            <Button type="button" variant="ghost" onClick={onClose}>{t('common.cancel')}</Button>
            <Button type="submit" isLoading={setQuota.isPending}>{t('common.save')}</Button>
          </div>
        </div>
      </form>
    </Modal>
  );
}
