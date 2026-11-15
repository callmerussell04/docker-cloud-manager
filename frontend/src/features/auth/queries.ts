import { useQuery } from '@tanstack/react-query';
import { getAuthConfigFn } from './api';

export const authKeys = {
  config: ['auth', 'config'] as const,
};

export function useAuthConfig() {
  return useQuery({
    queryKey: authKeys.config,
    queryFn: getAuthConfigFn,
  });
}
