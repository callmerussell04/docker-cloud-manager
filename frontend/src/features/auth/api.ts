import { publicApi, logoutSession, acceptLogin } from '@/shared/api/client';
import { signOutDestination, type AuthResponse, type AuthConfig, type SignOutDestination } from './model';
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

/** Signs out of DCM and answers where the browser goes next. */
export const logoutFn = async (): Promise<SignOutDestination> => signOutDestination(await logoutSession());

/**
 * Sends the browser to Keycloak's sign-out after a DCM sign-out of a Keycloak
 * session, so that session ends too and Keycloak returns to the login page.
 * Answers false, doing nothing, when the destination is the login page.
 */
export function leaveForProviderSignOut(destination: SignOutDestination): boolean {
  if (destination.to !== 'provider') return false;
  window.location.assign(destination.url);
  return true;
}
