import { useMemo } from 'react';
import { en } from './en';
import { ru } from './ru';
import { useLanguageStore, type Locale } from '@/store/languageStore';

const dictionaries = { ru, en } as const;

export type TranslationKey = keyof typeof ru;
export type TFunction = (key: TranslationKey, params?: Record<string, string | number>) => string;

export function translate(locale: Locale, key: TranslationKey, params?: Record<string, string | number>) {
  const template = dictionaries[locale][key] ?? ru[key] ?? key;
  if (!params) return template;

  return Object.entries(params).reduce(
    (value, [param, replacement]) => value.replaceAll(`{${param}}`, String(replacement)),
    template
  );
}

export function useT(): TFunction {
  const language = useLanguageStore((state) => state.language);

  return useMemo(() => {
    return (key, params) => translate(language, key, params);
  }, [language]);
}

export function useLocale() {
  return useLanguageStore((state) => state.language);
}

export function dateLocale(locale: Locale) {
  return locale === 'ru' ? 'ru-RU' : 'en-US';
}

export function statusLabel(t: TFunction, status?: string) {
  switch (status) {
    case 'running':
      return t('status.running');
    case 'exited':
      return t('status.exited');
    case 'stopped':
      return t('status.stopped');
    case 'created':
      return t('status.created');
    case 'creating':
      return t('status.creating');
    case 'starting':
      return t('status.starting');
    case 'stopping':
      return t('status.stopping');
    case 'deleting':
      return t('status.deleting');
    case 'missing':
      return t('status.missing');
    case 'reconciling':
      return t('status.reconciling');
    case 'error':
      return t('status.error');
    case 'available':
      return t('status.available');
    case 'pending':
      return t('status.pending');
    case 'success':
      return t('status.success');
    case 'failed':
      return t('status.failed');
    case 'failed_timeout':
      return t('status.failedTimeout');
    case 'failed_quota_exceeded':
      return t('status.failedQuotaExceeded');
    case 'failed_internal':
      return t('status.failedInternal');
    case 'canceled':
      return t('status.canceled');
    case 'canceling':
      return t('status.canceling');
    case 'building':
      return t('status.building');
    case 'deploying':
      return t('status.deploying');
    case 'active':
      return t('status.active');
    case 'deactivated':
      return t('status.deactivated');
    default:
      return status || t('status.unknown');
  }
}
