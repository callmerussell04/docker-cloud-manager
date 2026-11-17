import { expect, test } from 'vitest';
import { type AuthConfig, providerAvailability, unreachableProviders } from './model';

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
