import axios from 'axios';
import type { TFunction, TranslationKey } from '@/lib/i18n';

export interface ApiErrorResponse {
  error?: string;
  error_code?: string;
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
  'user memory quota exceeded': 'quota_exceeded',
  'server capacity reached, cannot allocate resources': 'resource_exhausted',
  'host server is out of memory': 'host_exhausted',
  'resource is currently in use': 'resource_in_use',
  conflict: 'conflict',
  'request timed out': 'timeout',
  'service unavailable': 'unavailable',
  'internal server error': 'internal',
};

export function getApiErrorMessage(error: unknown, fallback: string, t?: TFunction): ApiErrorMessage {
  if (axios.isAxiosError<ApiErrorResponse>(error)) {
    const responseError = error.response?.data?.error;
    const responseErrorCode = error.response?.data?.error_code;
    const code = normalizeErrorCode(responseErrorCode) ?? normalizeLegacyError(responseError);

    if (code && t) {
      return { message: t(`apiError.${code}` as TranslationKey) };
    }

    if (responseError && isSafeUserMessage(responseError)) {
      return { message: responseError };
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
