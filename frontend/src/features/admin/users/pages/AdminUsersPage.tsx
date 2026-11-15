import { useState } from 'react';
import { Edit, Gauge, Plus, PowerOff, RefreshCcw, RotateCcw, UserX, Users } from 'lucide-react';

import { Button } from '@/shared/ui/Button';
import { Pagination } from '@/shared/ui/Pagination';
import { Badge } from '@/shared/ui/Badge';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { cn } from '@/shared/lib/utils';
import { useT } from '@/shared/i18n';
import { userStatusLabel, type AdminUser } from '../model';
import { userTableLayout } from '../components/tableLayout';
import { UserFormModal } from '../components/UserFormModal';
import { UserQuotaModal } from '../components/UserQuotaModal';
import { useAdminUsers, useDeactivateAdminUser, useReactivateAdminUser } from '../queries';

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
          <div className={userTableLayout.minWidth}>
            <div className={cn("grid items-center gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500", userTableLayout.grid)}>
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
                <div key={user.user_id} className={cn("grid gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center", userTableLayout.grid)}>
                  <div className="font-medium truncate">{user.username}</div>
                  <div className="min-w-0">
                    <div className="truncate">{user.email}</div>
                    <div className="text-xs font-mono text-slate-500 truncate">{user.user_id}</div>
                  </div>
                  <div><Badge variant={user.role === 'admin' ? 'error' : 'default'}>{user.role}</Badge></div>
                  <div><Badge variant={user.status === 'active' ? 'success' : 'default'}>{userStatusLabel(t, user.status)}</Badge></div>
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

      <UserFormModal
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
