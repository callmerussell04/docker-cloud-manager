import { expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import { isVolumeBusy, volumeDeleteConsequence } from './model';

const t = ((key: string, params?: Record<string, string>) => (params ? `${key} ${JSON.stringify(params)}` : key)) as TFunction;

test.each([
  ['creating', true],
  ['deleting', true],
  ['available', false],
  ['missing', false],
  ['error', false],
])('isVolumeBusy(%s) -> %s', (status, expected) => {
  expect(isVolumeBusy(status)).toBe(expected);
});

test('deleting a measured volume names the size of the data it loses', () => {
  expect(volumeDeleteConsequence(t, { used_bytes: 3 * 1024 ** 3, usage_observed_at: 1_700_000_000 }))
    .toBe('volumes.deleteConsequence {"size":"3 GB"}');
});

test('deleting a volume whose usage was never measured says its data is lost, without a size', () => {
  expect(volumeDeleteConsequence(t, { used_bytes: 0, usage_observed_at: 0 })).toBe('volumes.deleteConsequenceUnmeasured');
});
