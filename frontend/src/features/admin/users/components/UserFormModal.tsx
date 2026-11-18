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
import { type AdminUser, providerManagedFields } from '../model';
import { type AdminUserForm, adminUserSchema } from '../schemas';
import { useCreateAdminUser, useUpdateAdminUser } from '../queries';

/** Creates a user, or edits one when a user is given. */
export function UserFormModal({ isOpen, user, onClose }: { isOpen: boolean; user: AdminUser | null; onClose: () => void }) {
  const addToast = useToastStore((state) => state.addToast);
  const isEdit = !!user;
  const t = useT();
  // Fields the identity provider owns are shown but not editable: the server
  // refuses to change them, and a change would be undone at the next sign-in.
  const managed = providerManagedFields(user);

  const { register, handleSubmit, reset, formState: { errors } } = useForm<AdminUserForm>({
    resolver: zodResolver(adminUserSchema(t)),
    defaultValues: {
      role: 'user',
      status: 'active',
    },
  });

  // Reset on every opening, so a second "create" does not show the first one's values.
  useEffect(() => {
    if (!isOpen) return;
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
  }, [isOpen, user, reset]);

  const createMutation = useCreateAdminUser();
  const updateMutation = useUpdateAdminUser();

  const onSubmit = (data: AdminUserForm) => {
    if (!isEdit && !data.password) {
      addToast(t('validation.passwordRequired'), 'error');
      return;
    }
    const payload = { ...data, password: data.password || undefined };
    if (user && managed.profile) {
      Object.assign(payload, { username: user.username, email: user.email, password: undefined });
    }
    if (user && managed.role) {
      payload.role = user.role;
    }
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
          {managed.profile && (
            <p className="md:col-span-2 text-sm text-slate-500 dark:text-slate-400">{t('admin.users.profileManagedHint')}</p>
          )}
          <div className="space-y-2">
            <Label htmlFor="username">{t('form.username')}</Label>
            {managed.profile ? (
              <Input id="username" key={`username-${user?.user_id}`} defaultValue={user?.username} disabled />
            ) : (
              <Input id="username" {...register('username')} error={!!errors.username} />
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="email">{t('form.email')}</Label>
            {managed.profile ? (
              <Input id="email" key={`email-${user?.user_id}`} type="email" defaultValue={user?.email} disabled />
            ) : (
              <Input id="email" type="email" {...register('email')} error={!!errors.email} />
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="password">{t('form.password')}</Label>
            {managed.profile ? (
              <Input id="password" type="password" disabled />
            ) : (
              <Input id="password" type="password" placeholder={isEdit ? t('form.passwordKeep') : ''} {...register('password')} error={!!errors.password} />
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="role">{t('admin.users.role')}</Label>
            {managed.role ? (
              <Select id="role" key={`role-${user?.user_id}`} defaultValue={user?.role} disabled aria-describedby="role-managed-hint">
                <option value="user">user</option>
                <option value="admin">admin</option>
              </Select>
            ) : (
              <Select id="role" {...register('role')}>
                <option value="user">user</option>
                <option value="admin">admin</option>
              </Select>
            )}
            {managed.role && (
              <p id="role-managed-hint" className="text-xs text-slate-500 dark:text-slate-400">{t('admin.users.roleManagedHint')}</p>
            )}
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
