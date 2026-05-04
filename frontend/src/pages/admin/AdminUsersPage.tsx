/* eslint-disable @typescript-eslint/no-explicit-any */
import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { Edit, Plus, RefreshCcw, RotateCcw, UserX, Users } from 'lucide-react';

import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Label } from '@/components/ui/Label';
import { Modal } from '@/components/ui/Modal';
import { Pagination } from '@/components/ui/Pagination';
import { Select } from '@/components/ui/Select';
import { Badge } from '@/components/ui/Badge';
import { useToastStore } from '@/store/toastStore';
import {
  createAdminUserFn,
  deactivateAdminUserFn,
  getAdminUsersFn,
  reactivateAdminUserFn,
  updateAdminUserFn,
} from '@/features/admin/api';
import { type AdminUser, type AdminUserForm, adminUserSchema } from '@/features/admin/types';

export function AdminUsersPage() {
  const [page, setPage] = useState(1);
  const [editingUser, setEditingUser] = useState<AdminUser | null>(null);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const limit = 20;
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);

  const { data, isFetching, refetch } = useQuery({
    queryKey: ['admin_users', page],
    queryFn: () => getAdminUsersFn(page, limit),
  });

  const deactivateMutation = useMutation({
    mutationFn: deactivateAdminUserFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_users'] });
      addToast('Пользователь деактивирован', 'success');
    },
    onError: (error: any) => addToast(error.response?.data?.error || 'Ошибка деактивации', 'error'),
  });

  const reactivateMutation = useMutation({
    mutationFn: reactivateAdminUserFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_users'] });
      addToast('Пользователь активирован', 'success');
    },
    onError: (error: any) => addToast(error.response?.data?.error || 'Ошибка активации', 'error'),
  });

  const users = data?.items || [];

  return (
    <div className="space-y-6 flex flex-col h-full">
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 shrink-0">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-red-600 dark:text-red-400 flex items-center gap-3">
            <Users className="w-8 h-8" />
            Пользователи
          </h1>
          <p className="text-slate-500 dark:text-slate-400 mt-1">Роли, статус и квоты пользователей</p>
        </div>
        <div className="flex items-center gap-3">
          <Button variant="secondary" onClick={() => refetch()} isLoading={isFetching} className="px-3">
            <RefreshCcw className="w-4 h-4" />
          </Button>
          <Button onClick={() => setIsCreateOpen(true)}>
            <Plus className="w-4 h-4 mr-2" />
            Создать
          </Button>
        </div>
      </div>

      <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl overflow-hidden flex-1 flex flex-col">
        <div className="overflow-x-auto flex-1">
          <div className="min-w-[950px]">
            <div className="grid grid-cols-[1.5fr_2fr_1fr_1fr_1.5fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 bg-slate-50/50 dark:bg-slate-800/50 text-sm font-medium text-slate-500">
              <div>Username</div>
              <div>Email / ID</div>
              <div>Role</div>
              <div>Status</div>
              <div>Quotas</div>
              <div className="text-right pr-2">Управление</div>
            </div>

            {isFetching ? (
              <div className="p-12 flex justify-center opacity-50"><RefreshCcw className="w-8 h-8 animate-spin" /></div>
            ) : users.length > 0 ? (
              users.map((user) => (
                <div key={user.user_id} className="grid grid-cols-[1.5fr_2fr_1fr_1fr_1.5fr_auto] gap-4 p-4 border-b border-white/20 dark:border-slate-700/50 hover:bg-white/20 dark:hover:bg-slate-800/30 items-center">
                  <div className="font-medium truncate">{user.username}</div>
                  <div className="min-w-0">
                    <div className="truncate">{user.email}</div>
                    <div className="text-xs font-mono text-slate-500 truncate">{user.user_id}</div>
                  </div>
                  <div><Badge variant={user.role === 'admin' ? 'error' : 'default'}>{user.role}</Badge></div>
                  <div><Badge variant={user.status === 'active' ? 'success' : 'default'}>{user.status}</Badge></div>
                  <div className="text-xs text-slate-500 space-y-1">
                    <div>CPU: {user.quota_cpu}</div>
                    <div>RAM: {user.quota_ram_mb} MB</div>
                    <div>Disk: {user.quota_disk_mb} MB</div>
                  </div>
                  <div className="flex gap-2 justify-end shrink-0">
                    <Button variant="secondary" className="h-8 px-2" onClick={() => setEditingUser(user)}>
                      <Edit className="w-4 h-4" />
                    </Button>
                    {user.status === 'active' ? (
                      <Button variant="danger" className="h-8 px-2" disabled={deactivateMutation.isPending} onClick={() => deactivateMutation.mutate(user.user_id)}>
                        <UserX className="w-4 h-4" />
                      </Button>
                    ) : (
                      <Button variant="secondary" className="h-8 px-2" disabled={reactivateMutation.isPending} onClick={() => reactivateMutation.mutate(user.user_id)}>
                        <RotateCcw className="w-4 h-4" />
                      </Button>
                    )}
                  </div>
                </div>
              ))
            ) : (
              <div className="p-12 text-center text-slate-500">Пользователи не найдены</div>
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
    </div>
  );
}

function AdminUserModal({ isOpen, user, onClose }: { isOpen: boolean; user: AdminUser | null; onClose: () => void }) {
  const queryClient = useQueryClient();
  const addToast = useToastStore((state) => state.addToast);
  const isEdit = !!user;

  const { register, handleSubmit, reset, formState: { errors } } = useForm<AdminUserForm>({
    resolver: zodResolver(adminUserSchema),
    defaultValues: {
      role: 'user',
      status: 'active',
      quota_cpu: 1,
      quota_ram_mb: 512,
      quota_disk_mb: 1024,
    },
  });

  useEffect(() => {
    if (user) {
      reset({ ...user, password: '' });
    } else {
      reset({
        username: '',
        email: '',
        password: '',
        role: 'user',
        status: 'active',
        quota_cpu: 1,
        quota_ram_mb: 512,
        quota_disk_mb: 1024,
      });
    }
  }, [user, reset]);

  const createMutation = useMutation({
    mutationFn: createAdminUserFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_users'] });
      addToast('Пользователь создан', 'success');
      onClose();
    },
    onError: (error: any) => addToast(error.response?.data?.error || 'Ошибка создания пользователя', 'error'),
  });

  const updateMutation = useMutation({
    mutationFn: updateAdminUserFn,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['admin_users'] });
      addToast('Пользователь обновлен', 'success');
      onClose();
    },
    onError: (error: any) => addToast(error.response?.data?.error || 'Ошибка обновления пользователя', 'error'),
  });

  const onSubmit = (data: AdminUserForm) => {
    if (!isEdit && !data.password) {
      addToast('Пароль обязателен для нового пользователя', 'error');
      return;
    }
    const payload = { ...data, password: data.password || undefined };
    if (isEdit && user) {
      updateMutation.mutate({ id: user.user_id, data: payload });
    } else {
      createMutation.mutate(payload);
    }
  };

  return (
    <Modal isOpen={isOpen} onClose={onClose} title={isEdit ? 'Редактировать пользователя' : 'Создать пользователя'} className="max-w-2xl">
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-5">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="username">Username</Label>
            <Input id="username" {...register('username')} error={!!errors.username} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="email">Email</Label>
            <Input id="email" type="email" {...register('email')} error={!!errors.email} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="password">Password</Label>
            <Input id="password" type="password" placeholder={isEdit ? 'Не менять' : ''} {...register('password')} error={!!errors.password} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="role">Role</Label>
            <Select id="role" {...register('role')}>
              <option value="user">user</option>
              <option value="admin">admin</option>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="status">Status</Label>
            <Select id="status" {...register('status')}>
              <option value="active">active</option>
              <option value="deactivated">deactivated</option>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="quota_cpu">CPU quota</Label>
            <Input id="quota_cpu" type="number" step="0.1" {...register('quota_cpu', { valueAsNumber: true })} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="quota_ram_mb">RAM quota MB</Label>
            <Input id="quota_ram_mb" type="number" {...register('quota_ram_mb', { valueAsNumber: true })} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="quota_disk_mb">Disk quota MB</Label>
            <Input id="quota_disk_mb" type="number" {...register('quota_disk_mb', { valueAsNumber: true })} />
          </div>
        </div>
        <div className="flex justify-end gap-3 pt-4 border-t border-slate-200 dark:border-slate-700/50">
          <Button type="button" variant="ghost" onClick={onClose}>Отмена</Button>
          <Button type="submit" isLoading={createMutation.isPending || updateMutation.isPending}>Сохранить</Button>
        </div>
      </form>
    </Modal>
  );
}
