import { z } from 'zod';
import type { TFunction } from '@/shared/i18n';

export const adminUserSchema = (t: TFunction) => z.object({
  username: z.string().min(1, t('validation.nameRequired')),
  email: z.string().email(t('validation.emailInvalid')),
  password: z.string().optional(),
  role: z.enum(['admin', 'user']),
  status: z.enum(['active', 'deactivated']),
});

export type AdminUserForm = z.input<ReturnType<typeof adminUserSchema>>;

export const userQuotaSchema = (t: TFunction) => z.object({
  quota_cpu: z.number({ error: t('validation.quotaPositive') }).min(0.001, t('validation.quotaPositive')).max(1_000_000, t('validation.quotaPositive')),
  quota_ram_mb: z.number({ error: t('validation.quotaPositive') }).int().min(1, t('validation.quotaPositive')),
  quota_disk_mb: z.number({ error: t('validation.quotaPositive') }).int().min(1, t('validation.quotaPositive')),
});

export type UserQuotaForm = z.input<ReturnType<typeof userQuotaSchema>>;
