import axios, { type InternalAxiosRequestConfig } from 'axios';
import { API_URL } from '@/config';
import { useAuthStore } from '@/store/authStore';
import { createSessionCoordinator } from '@/shared/api/sessionCoordinator';

interface RetryableRequestConfig extends InternalAxiosRequestConfig {
  _retry?: boolean;
}



export const publicApi = axios.create({
  baseURL: API_URL,
  headers: {
    'Content-Type': 'application/json',
  },
  withCredentials: true,
});

export const privateApi = axios.create({
  baseURL: API_URL,
  headers: {
    'Content-Type': 'application/json',
  },
  withCredentials: true,
});

const logoutChannel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel('dcm-session');

const session = createSessionCoordinator({
  refresh: async () => {
    const response = await publicApi.post<{ access_token: string }>('/auth/refresh', undefined, { timeout: 15000 });
    if (!response.data.access_token) throw new Error('Session response unavailable');
    return response.data.access_token;
  },
  logout: async () => {
    await publicApi.post('/auth/logout', undefined, { timeout: 15000 });
    logoutChannel?.postMessage('logout');
  },
  withLock: (operation) => navigator.locks
    ? navigator.locks.request('dcm-session-cookie', operation)
    : operation(),
  onToken: (token) => useAuthStore.getState().setAccessToken(token),
  onLogout: () => useAuthStore.getState().logout(),
  onSuspend: (action) => useAuthStore.getState().suspendSession(action),
  isUnauthorized: (error) => axios.isAxiosError(error) && error.response?.status === 401,
});

if (logoutChannel) logoutChannel.onmessage = (event) => {
  if (event.data === 'logout') session.invalidate();
};

export const refreshSession = () => session.refresh();
export const retrySessionRefresh = () => session.refresh(true);
export const logoutSession = () => session.logout();
export const acceptLogin = (token: string) => session.acceptLogin(token);

privateApi.interceptors.request.use((config) => {
  const state = useAuthStore.getState();
  if (state.sessionIssue) return Promise.reject(new Error('Session temporarily unavailable'));
  if (state.accessToken) config.headers.Authorization = `Bearer ${state.accessToken}`;
  return config;
});

privateApi.interceptors.response.use(
  (response) => response,
  async (error) => {
    const originalRequest = error.config as RetryableRequestConfig | undefined;
    if (error.response?.status === 401 && originalRequest && !originalRequest._retry) {
      originalRequest._retry = true;
      const token = await refreshSession();
      originalRequest.headers.Authorization = `Bearer ${token}`;
      return privateApi(originalRequest);
    }
    return Promise.reject(error);
  },
);
