import { publicApi } from '@/api/axios';
import type { LoginData, RegisterData, AuthResponse } from '../types';

export const loginFn = async (data: LoginData): Promise<AuthResponse> => {
  const response = await publicApi.post<AuthResponse>('/auth/login', data);
  return response.data;
};

export const registerFn = async (data: RegisterData): Promise<AuthResponse> => {
  const response = await publicApi.post<AuthResponse>('/auth/register', data);
  return response.data;
};

export const logoutFn = async (): Promise<void> => {
  await publicApi.post('/auth/logout');
};