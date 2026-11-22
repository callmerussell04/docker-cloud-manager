import axios from 'axios';
import type { TFunction, TranslationKey } from '@/shared/i18n';
import { SessionUnavailableError } from '@/shared/api/sessionCoordinator';

export interface ApiErrorResponse {
  error?: string;
  error_code?: string;
  // The limit a rejected request exceeded, when the server names one.
  limit?: string;
  request_id?: string;
}

export interface ApiErrorMessage {
  message: string;
}

const knownErrorCodes = new Set([
  'bad_request',
  'not_found',
  'already_exists',
  'invalid_credentials',
  'invalid_token',
  'unauthorized',
  'forbidden',
  'invalid_file_format',
  'limit_exceeded',
  'quota_exceeded',
  'resource_exhausted',
  'host_exhausted',
  'resource_in_use',
  'conflict',
  'timeout',
  'unavailable',
  'internal',
]);

// The limits the backend names in a rejection (apperrors.Limit). Each has its
// own message, `apiError.limit.<name>`.
export const knownLimits = [
  'user_memory',
  'user_cpu',
  'user_disk',
  'host_memory',
  'host_cpu',
  'disk_floor',
  'containers_per_user',
  'volumes_per_user',
  'queued_container_creates',
  'queued_builds',
  'queued_compose_deployments',
  'staged_source_bytes',
] as const;

type Limit = (typeof knownLimits)[number];

function normalizeLimit(limit?: string): Limit | undefined {
  return knownLimits.find((known) => known === limit);
}

const legacyErrorCodeByMessage: Record<string, string> = {
  'bad request': 'bad_request',
  'not found': 'not_found',
  'already exists': 'already_exists',
  'invalid credentials': 'invalid_credentials',
  'invalid token': 'invalid_token',
  unauthorized: 'unauthorized',
  forbidden: 'forbidden',
  'invalid file format, allowed: .zip, .tar, .tar.gz': 'invalid_file_format',
  'maximum number of resources reached': 'limit_exceeded',
  'user quota exceeded': 'quota_exceeded',
  'server capacity reached, cannot allocate resources': 'resource_exhausted',
  'host capacity exhausted': 'host_exhausted',
  'resource is currently in use': 'resource_in_use',
  conflict: 'conflict',
  'request timed out': 'timeout',
  'service unavailable': 'unavailable',
  'internal server error': 'internal',
};

export function getApiErrorMessage(error: unknown, fallback: string, t?: TFunction): ApiErrorMessage {
  if (error instanceof SessionUnavailableError) {
    return { message: t ? t(error.messageKey) : fallback };
  }

  if (axios.isAxiosError<ApiErrorResponse>(error)) {
    const responseError = error.response?.data?.error;
    const responseErrorCode = error.response?.data?.error_code;
    const code = normalizeErrorCode(responseErrorCode) ?? normalizeLegacyError(responseError);
    const limit = normalizeLimit(error.response?.data?.limit);
    const detailedServerMessage = getDetailedServerErrorMessage(responseError, t);

    // A named limit says more than its error code, which several limits share.
    if (limit && t) {
      return { message: t(`apiError.limit.${limit}`) };
    }

    if (detailedServerMessage) {
      return { message: detailedServerMessage };
    }

    if (code && t) {
      return { message: t(`apiError.${code}` as TranslationKey) };
    }

    const serverMessage = getServerErrorMessage(responseError, t);
    if (serverMessage) {
      return { message: serverMessage };
    }

    if (error.response) {
      return { message: fallback };
    }

    if (error.code === 'ECONNABORTED') {
      return { message: t ? t('apiError.noServerResponse') : 'No response from server' };
    }

    if (error.request) {
      return { message: t ? t('apiError.serverUnavailable') : 'Server is unavailable' };
    }
  }

  return { message: fallback };
}

// failureReason is the reason an error notification shows under its title:
// the mapped message for the error, or undefined when the mapping has nothing
// more specific to say. It is never raw server text.
export function failureReason(error: unknown, t: TFunction): string | undefined {
  return getApiErrorMessage(error, '', t).message || undefined;
}

// isNotFoundError reports whether a request failed because the resource does
// not exist, or is not the caller's to see.
export function isNotFoundError(error: unknown) {
  return axios.isAxiosError(error) && error.response?.status === 404;
}

export function getServerErrorMessage(message?: string, t?: TFunction) {
  if (!message || !isSafeUserMessage(message)) return undefined;

  return getDetailedServerErrorMessage(message, t) ?? message;
}

function getDetailedServerErrorMessage(message?: string, t?: TFunction) {
  if (!message || !isSafeUserMessage(message)) return undefined;

  // Gateway's answer when SSO deactivated the account but Core could not stop
  // its workloads. An SSO outage is also 503, so the message, not the status,
  // tells them apart.
  if (/^the account is deactivated, but its workloads could not be stopped/i.test(message)) {
    return t ? t('apiError.workloadsNotStopped') : message;
  }

  // SSO's fixed refusals of an edit to what the identity provider owns.
  if (/^role of this user is managed by the identity provider$/i.test(message)) {
    return t ? t('apiError.roleManagedByProvider') : message;
  }
  if (/^username, email and password of this user are managed by the identity provider$/i.test(message)) {
    return t ? t('apiError.profileManagedByProvider') : message;
  }

  const subdomainInUse = /^subdomain\s+(.+?)\s+is already in use$/i.exec(message);
  if (subdomainInUse) {
    return t ? t('apiError.subdomainInUse', { subdomain: subdomainInUse[1] }) : message;
  }

  const duplicateComposeSubdomain = /^subdomain\s+(.+?)\s+is used by both services\s+(.+?)\s+and\s+(.+)$/i.exec(message);
  if (duplicateComposeSubdomain) {
    return t ? t('apiError.subdomainUsedByServices', {
      subdomain: duplicateComposeSubdomain[1],
      first: duplicateComposeSubdomain[2],
      second: duplicateComposeSubdomain[3],
    }) : message;
  }

  return undefined;
}

function isSafeUserMessage(message: string) {
  const lower = message.toLowerCase();
  if (lower.includes('request_id') || lower.includes('trace') || lower.includes('stack')) return false;
  if (lower.includes('sql') || lower.includes('grpc') || lower.includes('docker')) return false;
  if (lower.includes('internal') || lower.includes('database')) return false;
  return message.length <= 160;
}

function normalizeErrorCode(code?: string) {
  if (!code) return undefined;
  return knownErrorCodes.has(code) ? code : undefined;
}

function normalizeLegacyError(message?: string) {
  if (!message) return undefined;

  const direct = legacyErrorCodeByMessage[message.toLowerCase()];
  if (direct) return direct;

  const lower = message.toLowerCase();
  if (lower.includes('used by')) return 'resource_in_use';
  if (lower.includes('quota')) return 'quota_exceeded';
  if (lower.includes('timed out') || lower.includes('timeout')) return 'timeout';

  return undefined;
}
