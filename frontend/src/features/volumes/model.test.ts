import { expect, test } from 'vitest';
import { isVolumeBusy } from './model';

test.each([
  ['creating', true],
  ['deleting', true],
  ['available', false],
  ['missing', false],
  ['error', false],
])('isVolumeBusy(%s) -> %s', (status, expected) => {
  expect(isVolumeBusy(status)).toBe(expected);
});
