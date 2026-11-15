import { useEffect, useMemo, useRef, type RefObject } from 'react';
import { en } from './locales/en';
import { ru } from './locales/ru';
import { useLanguageStore, type Locale } from '@/shared/lib/languageStore';

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

// useTRef holds the current translator for effects that must not restart when
// the language changes, such as an open log stream or terminal session. Read
// it as tRef.current inside the effect and its callbacks.
export function useTRef(): RefObject<TFunction> {
  const t = useT();
  const ref = useRef(t);
  useEffect(() => {
    ref.current = t;
  }, [t]);
  return ref;
}

export function useLocale() {
  return useLanguageStore((state) => state.language);
}

export function dateLocale(locale: Locale) {
  return locale === 'ru' ? 'ru-RU' : 'en-US';
}

// labelFor translates a value through a map of known values to keys, such as
// a feature's statuses, and shows an unknown value as it is.
export function labelFor(t: TFunction, keys: Readonly<Record<string, TranslationKey>>, value?: string) {
  const key = value ? keys[value] : undefined;
  return key ? t(key) : value || t('status.unknown');
}
