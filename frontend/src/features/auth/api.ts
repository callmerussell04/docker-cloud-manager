import { publicApi, logoutSession, acceptLogin } from '@/shared/api/client';
import type { AuthResponse, AuthConfig } from './model';
import type { LoginData, RegisterData } from './schemas';

export const loginFn = async (data: LoginData): Promise<AuthResponse> => {
  const response = await publicApi.post<AuthResponse>('/auth/login', data);
  if (!response.data.access_token) throw new Error('Session response unavailable');
  acceptLogin(response.data.access_token);
  return response.data;
};

export const registerFn = async (data: RegisterData): Promise<AuthResponse> => {
  const response = await publicApi.post<AuthResponse>('/auth/register', data);
  return response.data;
};

export const getAuthConfigFn = async (): Promise<AuthConfig> => {
  const response = await publicApi.get<AuthConfig>('/auth/providers');
  return response.data;
};

export const logoutFn = async (): Promise<void> => {
  await logoutSession();
};
