import { AxiosError, AxiosHeaders } from 'axios';
import { expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import { en } from '@/shared/i18n/locales/en';
import { ru } from '@/shared/i18n/locales/ru';
import { getApiErrorMessage, knownLimits, type ApiErrorResponse } from './apiError';

// The translator answers with the key, so a test sees which message was chosen.
const t = ((key: string) => key) as TFunction;

function rejection(status: number, data: ApiErrorResponse) {
  const config = { headers: new AxiosHeaders() };
  return new AxiosError('request failed', 'ERR_BAD_RESPONSE', config, {}, {
    status, statusText: '', data, headers: {}, config,
  });
}

test('a rejection that names its limit shows that limit\'s message', () => {
  const error = rejection(409, { error: 'user CPU quota exceeded', error_code: 'quota_exceeded', limit: 'user_cpu' });

  expect(getApiErrorMessage(error, 'fallback', t).message).toBe('apiError.limit.user_cpu');
});

test('a host limit shows its own message, not the generic host one', () => {
  const error = rejection(503, { error: 'host is low on free disk', error_code: 'host_exhausted', limit: 'disk_floor' });

  expect(getApiErrorMessage(error, 'fallback', t).message).toBe('apiError.limit.disk_floor');
});

test('an unknown limit falls back to the error code', () => {
  const error = rejection(409, { error: 'user quota exceeded', error_code: 'quota_exceeded', limit: 'made_up' });

  expect(getApiErrorMessage(error, 'fallback', t).message).toBe('apiError.quota_exceeded');
});

test('a rejection without a limit shows the error code message', () => {
  const error = rejection(409, { error: 'maximum number of resources reached', error_code: 'limit_exceeded' });

  expect(getApiErrorMessage(error, 'fallback', t).message).toBe('apiError.limit_exceeded');
});

test('the kinds\' neutral texts map to their codes when no code is sent', () => {
  expect(getApiErrorMessage(rejection(409, { error: 'user quota exceeded' }), 'fallback', t).message).toBe('apiError.quota_exceeded');
  expect(getApiErrorMessage(rejection(503, { error: 'host capacity exhausted' }), 'fallback', t).message).toBe('apiError.host_exhausted');
});

test('every limit the backend names has a message in both locales', () => {
  expect([...knownLimits].sort()).toEqual([
    'containers_per_user', 'disk_floor', 'host_cpu', 'host_memory', 'queued_builds',
    'queued_compose_deployments', 'queued_container_creates', 'staged_source_bytes',
    'user_cpu', 'user_disk', 'user_memory', 'volumes_per_user',
  ]);
  for (const limit of knownLimits) {
    expect(ru, limit).toHaveProperty([`apiError.limit.${limit}`]);
    expect(en, limit).toHaveProperty([`apiError.limit.${limit}`]);
  }
});

test.each([
  ['role of this user is managed by the identity provider', 'apiError.roleManagedByProvider'],
  ['username, email and password of this user are managed by the identity provider', 'apiError.profileManagedByProvider'],
])('SSO\'s refusal of a provider-managed edit shows its own message: %s', (message, key) => {
  const error = rejection(400, { error: message, error_code: 'bad_request' });
  expect(getApiErrorMessage(error, 'fallback', t).message).toBe(key);
});
