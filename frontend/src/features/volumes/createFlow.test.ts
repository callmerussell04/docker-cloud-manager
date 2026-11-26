import { describe, expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import { diskQuotaFit, volumeStepIssues } from './createFlow';

const t = ((key: string, params?: Record<string, string>) => (params ? `${key} ${JSON.stringify(params)}` : key)) as TFunction;
const MiB = 1024 * 1024;
const quota = { usedBytes: 700 * MiB, quotaBytes: 1000 * MiB };

describe('diskQuotaFit', () => {
  test('places a typed size against what the quota leaves', () => {
    expect(diskQuotaFit(quota, '200')).toEqual({ heldBytes: 700 * MiB, addBytes: 200 * MiB, availableBytes: 300 * MiB, freeBytes: 100 * MiB, fits: true });
  });

  test('a size beyond what is left does not fit and leaves nothing free', () => {
    expect(diskQuotaFit(quota, '301')).toMatchObject({ addBytes: 301 * MiB, freeBytes: 0, fits: false });
  });

  test('an empty or refused size adds nothing the bar can size', () => {
    expect(diskQuotaFit(quota, '')).toMatchObject({ addBytes: undefined, freeBytes: 300 * MiB, fits: true });
    expect(diskQuotaFit(quota, '1.5')).toMatchObject({ addBytes: undefined, fits: true });
  });

  test('a quota already overrun leaves nothing available', () => {
    expect(diskQuotaFit({ usedBytes: 1200 * MiB, quotaBytes: 1000 * MiB }, '')).toMatchObject({ availableBytes: 0, freeBytes: 0 });
  });
});

describe('volumeStepIssues', () => {
  test('the volume step asks for a name', () => {
    expect(volumeStepIssues({ name: '', size_mib: '' }, 'volume', t)).toEqual([{ field: 'name', message: 'validation.nameRequired' }]);
  });

  test('the volume step refuses a size that is not a whole number of MiB', () => {
    expect(volumeStepIssues({ name: 'data', size_mib: '0' }, 'volume', t)).toEqual([{ field: 'size_mib', message: 'volumes.sizeInvalid' }]);
  });

  test('the volume step refuses a size the disk quota cannot hold, when the quota is known', () => {
    expect(volumeStepIssues({ name: 'data', size_mib: '400' }, 'volume', t, quota))
      .toEqual([{ field: 'size_mib', message: 'validation.diskOverQuota {"free":"300 MB"}' }]);
    expect(volumeStepIssues({ name: 'data', size_mib: '400' }, 'volume', t)).toEqual([]);
  });

  test('the review step has nothing of its own to check', () => {
    expect(volumeStepIssues({ name: '', size_mib: '' }, 'review', t)).toEqual([]);
  });
});
