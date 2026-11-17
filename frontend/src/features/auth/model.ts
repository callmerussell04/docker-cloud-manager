export interface AuthResponse {
  access_token?: string;
  user_id?: string;
}

export interface AuthProviderData {
  name: string;
  /** False while SSO has not reached the enabled provider; sign-in through it is unavailable until then. */
  available: boolean;
}

export interface AuthConfig {
  local_login_enabled: boolean;
  local_register_enabled: boolean;
  oidc_providers: AuthProviderData[];
}

export type ProviderAvailability = 'off' | 'unavailable' | 'available';

/** Whether the named provider is off, enabled but not yet reached by SSO, or ready for sign-in. */
export function providerAvailability(config: AuthConfig | undefined, name: string): ProviderAvailability {
  const provider = config?.oidc_providers.find((candidate) => candidate.name === name);
  if (!provider) return 'off';
  return provider.available ? 'available' : 'unavailable';
}

/** Enabled providers SSO has not reached. */
export function unreachableProviders(config: AuthConfig | undefined): string[] {
  return config?.oidc_providers.filter((provider) => !provider.available).map((provider) => provider.name) ?? [];
}
