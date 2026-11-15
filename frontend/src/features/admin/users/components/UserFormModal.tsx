import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';

import { Button } from '@/shared/ui/Button';
import { Input } from '@/shared/ui/Input';
import { Label } from '@/shared/ui/Label';
import { Modal } from '@/shared/ui/Modal';
import { Select } from '@/shared/ui/Select';
import { useToastStore } from '@/shared/ui/toastStore';
import { useT } from '@/shared/i18n';
import type { AdminUser } from '../model';
import { type AdminUserForm, adminUserSchema } from '../schemas';
import { useCreateAdminUser, useUpdateAdminUser } from '../queries';

/** Creates a user, or edits one when a user is given. */
export function UserFormModal({ isOpen, user, onClose }: { isOpen: boolean; user: AdminUser | null; onClose: () => void }) {
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
