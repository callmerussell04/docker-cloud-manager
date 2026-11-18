import { describe, expect, test } from 'vitest';
import { translate } from '@/shared/i18n';
import { type AuthConfig, callbackResult, loginFailureMessages, providerAvailability, unreachableProviders } from './model';

const config = (providers: AuthConfig['oidc_providers']): AuthConfig => ({
  local_login_enabled: true,
  local_register_enabled: true,
  oidc_providers: providers,
});

test.each([
  ['off before the config has loaded', undefined, 'off'],
  ['off with OIDC disabled', config([]), 'off'],
  ['unavailable while SSO has not reached it', config([{ name: 'keycloak', available: false }]), 'unavailable'],
  ['available once reached', config([{ name: 'keycloak', available: true }]), 'available'],
  ['off for another provider', config([{ name: 'other', available: true }]), 'off'],
] as const)('providerAvailability: %s', (_, authConfig, expected) => {
  expect(providerAvailability(authConfig, 'keycloak')).toBe(expected);
});

test('unreachableProviders lists enabled providers SSO has not reached', () => {
  expect(unreachableProviders(undefined)).toEqual([]);
  expect(unreachableProviders(config([{ name: 'keycloak', available: true }]))).toEqual([]);
  expect(unreachableProviders(config([{ name: 'keycloak', available: false }]))).toEqual(['keycloak']);
});

describe('callbackResult', () => {
  test('reports success only for login=success', () => {
    expect(callbackResult('?login=success')).toEqual({ ok: true });
    expect(callbackResult('')).toEqual({ ok: false, message: 'auth.callback.failed' });
  });

  test.each([
    ['access_denied', 'auth.callback.reason.accessDenied'],
    ['provider_error', 'auth.callback.reason.providerError'],
    ['state_invalid', 'auth.callback.reason.stateInvalid'],
    ['identity_rejected', 'auth.callback.reason.identityRejected'],
    ['account_deactivated', 'auth.callback.reason.accountDeactivated'],
    ['account_conflict', 'auth.callback.reason.accountConflict'],
    ['provider_unavailable', 'auth.callback.reason.providerUnavailable'],
    ['rate_limited', 'auth.callback.reason.rateLimited'],
    ['login_failed', 'auth.callback.failed'],
    ['something_new', 'auth.callback.failed'],
  ] as const)('shows the message for reason %s', (reason, message) => {
    expect(callbackResult(`?login=error&reason=${reason}`)).toEqual({ ok: false, message });
  });

  test.each(['en', 'ru'] as const)('has every reason message in %s', (locale) => {
    for (const reason of Object.values(loginFailureMessages)) {
      const text = translate(locale, reason);
      expect(text).not.toBe(reason);
      expect(text.length).toBeGreaterThan(0);
    }
  });
});
