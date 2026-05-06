import { z } from 'zod';
import type { TFunction } from '@/lib/i18n';

export const createLoginSchema = (t: TFunction) => z.object({
  username: z.string().min(1, t('validation.usernameRequired')),
  password: z.string().min(6, t('validation.passwordMin6')),
});

export const createRegisterSchema = (t: TFunction) => z.object({
  username: z.string().min(3, t('validation.usernameMin3')),
  email: z.string().email(t('validation.emailInvalid')),
  password: z.string().min(6, t('validation.passwordMin6')),
});

export type LoginData = z.infer<ReturnType<typeof createLoginSchema>>;
export type RegisterData = z.infer<ReturnType<typeof createRegisterSchema>>;

export interface AuthResponse {
  access_token?: string;
  user_id?: string;
}
