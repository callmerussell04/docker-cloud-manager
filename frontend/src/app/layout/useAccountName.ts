import { useAuthStore } from '@/shared/api/sessionStore';
import { useT } from '@/shared/i18n';

// useAccountName is the signed-in user's name, or a generic one.
export function useAccountName() {
  const t = useT();
  const username = useAuthStore((state) => state.username);
  return username || t('common.user');
}
