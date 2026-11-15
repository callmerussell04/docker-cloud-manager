import { expect, test } from 'vitest';
import { isImageBusy } from './model';

test.each([
  ['building', true],
  ['deleting', true],
  ['available', false],
  ['missing', false],
  ['error', false],
])('isImageBusy(%s) -> %s', (status, expected) => {
  expect(isImageBusy(status)).toBe(expected);
});
