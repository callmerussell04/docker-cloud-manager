import { expect, test } from 'vitest';
import { translate } from '@/shared/i18n';
import { auditActionLabel, auditActions } from './reportFormat';

test('the audit filter offers every action Gateway records, including provider sign-out', () => {
  expect(auditActions).toContain('auth.provider_logout');
  expect(auditActions).toContain('user.quota_set');
  expect(auditActions).toContain('user.quota_reset');
});

test.each(['ru', 'en'] as const)('every audit action has its own %s label', (locale) => {
  const t = (key: Parameters<typeof translate>[1]) => translate(locale, key);
  const labels = auditActions.map((action) => auditActionLabel(t, action));
  for (const [index, label] of labels.entries()) expect(label, auditActions[index]).not.toBe(auditActions[index]);
  expect(new Set(labels).size).toBe(labels.length);
});

test('provider sign-out is labelled in both locales', () => {
  expect(auditActionLabel((key) => translate('en', key), 'auth.provider_logout')).toBe('Signed out by the identity provider');
  expect(auditActionLabel((key) => translate('ru', key), 'auth.provider_logout')).toBe('Выход по сигналу поставщика удостоверений');
});

test('an action the page does not know is shown as recorded', () => {
  expect(auditActionLabel((key) => translate('en', key), 'future.action')).toBe('future.action');
});
