import { useEffect } from 'react';
import { publicApi } from '@/api/axios';
import { useAuthStore } from '@/store/authStore';

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const { setAccessToken, setInitialized, logout } = useAuthStore();

  useEffect(() => {
    const initAuth = async () => {
      try {
        const response = await publicApi.post('/auth/refresh');
        if (response.data?.access_token) {
          setAccessToken(response.data.access_token);
        }
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      } catch (error) {
        logout();
      } finally {
        setInitialized(true);
      }
    };

    initAuth();
  }, [setAccessToken, setInitialized, logout]);

  return <>{children}</>;
}