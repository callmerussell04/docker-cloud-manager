import { useEffect } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { RotateCcw } from 'lucide-react';

import { Button } from '@/shared/ui/Button';
import { Input } from '@/shared/ui/Input';
import { Label } from '@/shared/ui/Label';
import { Modal } from '@/shared/ui/Modal';
import { useT } from '@/shared/i18n';
import type { AdminUser } from '../model';
import { type UserQuotaForm, userQuotaSchema } from '../schemas';
import { useResetAdminUserQuota, useSetAdminUserQuota } from '../queries';

/** Edits a user's quota through the quota routes; reset returns them to the default. */
export function UserQuotaModal({ user, onClose }: { user: AdminUser | null; onClose: () => void }) {
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
