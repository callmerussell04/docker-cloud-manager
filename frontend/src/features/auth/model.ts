import type { TranslationKey } from '@/shared/i18n';

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

/**
 * Messages for the reasons Gateway gives when a sign-in through the provider
 * fails (`login=error&reason=<code>` on the callback page). A reason not listed
 * here, including `login_failed`, shows the generic failure message.
 */
export const loginFailureMessages = {
  access_denied: 'auth.callback.reason.accessDenied',
  provider_error: 'auth.callback.reason.providerError',
  state_invalid: 'auth.callback.reason.stateInvalid',
  identity_rejected: 'auth.callback.reason.identityRejected',
  account_deactivated: 'auth.callback.reason.accountDeactivated',
  account_conflict: 'auth.callback.reason.accountConflict',
  provider_unavailable: 'auth.callback.reason.providerUnavailable',
  rate_limited: 'auth.callback.reason.rateLimited',
} as const satisfies Record<string, TranslationKey>;

export type SignOutDestination = { to: 'provider'; url: string } | { to: 'login' };

/**
 * Where the browser goes after DCM sign-out succeeded: for a session created
 * through Keycloak, the provider's sign-out URL Gateway answered, so the
 * Keycloak session ends too and returns to the login page; otherwise, or for
 * anything but an absolute http(s) URL, the login page.
 */
export function signOutDestination(providerLogoutUrl: unknown): SignOutDestination {
  if (typeof providerLogoutUrl !== 'string' || providerLogoutUrl === '') return { to: 'login' };
  try {
    const url = new URL(providerLogoutUrl);
    if (url.protocol === 'https:' || url.protocol === 'http:') return { to: 'provider', url: providerLogoutUrl };
  } catch {
    // Not an absolute URL.
  }
  return { to: 'login' };
}

export type CallbackResult = { ok: true } | { ok: false; message: TranslationKey };

/** Reads the callback page's query: only `login=success` signs in; anything else is a failure with a message. */
export function callbackResult(search: string): CallbackResult {
  const params = new URLSearchParams(search);
  if (params.get('login') === 'success') return { ok: true };
  const reason = params.get('reason') ?? '';
  const message = Object.hasOwn(loginFailureMessages, reason)
    ? loginFailureMessages[reason as keyof typeof loginFailureMessages]
    : 'auth.callback.failed';
  return { ok: false, message };
}
