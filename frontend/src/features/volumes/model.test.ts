import { describe, expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import {
  defaultVolumeSort,
  isVolumeBusy,
  matchesVolumeSearch,
  sortVolumes,
  volumeDeleteBlock,
  volumeDeleteConsequence,
  volumeTone,
  volumeUsersLabel,
  type VolumeData,
} from './model';

const t = ((key: string, params?: Record<string, string>) => (params ? `${key} ${JSON.stringify(params)}` : key)) as TFunction;

function volume(overrides: Partial<VolumeData> = {}): VolumeData {
  return {
    id: overrides.name ?? 'id',
    name: 'data',
    status: 'available',
    last_error: '',
    used_bytes: 0,
    usage_observed_at: 0,
    size_bytes: 1024 ** 3,
    size_enforced: false,
    created_at: 1_700_000_000,
    used_by: [],
    ...overrides,
  };
}

test.each([
  ['creating', true],
  ['deleting', true],
  ['available', false],
  ['missing', false],
  ['error', false],
])('isVolumeBusy(%s) -> %s', (status, expected) => {
  expect(isVolumeBusy(status)).toBe(expected);
});

test.each([
  ['available', 'ok'],
  ['creating', 'busy'],
  ['deleting', 'busy'],
  ['error', 'bad'],
  ['missing', 'bad'],
  ['something-new', 'busy'],
])('volumeTone(%s) -> %s', (status, tone) => {
  expect(volumeTone(status)).toBe(tone);
});

describe('volumeDeleteConsequence', () => {
  test('a measured volume no container uses names its size and that nothing uses it', () => {
    expect(volumeDeleteConsequence(t, volume({ used_bytes: 3 * 1024 ** 3, usage_observed_at: 1_700_000_000 })))
      .toBe('volumes.deleteConsequence {"size":"3 GB"} volumes.deleteUnused');
  });

  test('a volume whose usage was never measured names its reserved size as the most it loses', () => {
    expect(volumeDeleteConsequence(t, volume())).toBe('volumes.deleteConsequenceUnmeasured {"size":"1 GB"} volumes.deleteUnused');
  });

  test('a volume in use names the containers using it', () => {
    const used = volume({ used_bytes: 2 * 1024 ** 2, usage_observed_at: 1, used_by: [{ id: 'a', name: 'postgres' }, { id: 'b', name: 'backup' }] });
    expect(volumeDeleteConsequence(t, used))
      .toBe('volumes.deleteConsequence {"size":"2 MB"} volumes.deleteUsedBy {"names":"postgres, backup"}');
  });
});

describe('volumeUsersLabel', () => {
  test('names nothing for a volume no container uses', () => {
    expect(volumeUsersLabel(t, [])).toBeUndefined();
  });

  test('names up to two containers', () => {
    expect(volumeUsersLabel(t, [{ id: 'a', name: 'web' }, { id: 'b', name: 'worker' }])).toBe('web, worker');
  });

  test('names two and counts the rest', () => {
    const users = ['web', 'worker', 'cron', 'backup'].map((name) => ({ id: name, name }));
    expect(volumeUsersLabel(t, users)).toBe('volumes.usedByMore {"names":"web, worker","count":2}');
  });
});

describe('volumeDeleteBlock', () => {
  test('a free volume at rest can be deleted', () => {
    expect(volumeDeleteBlock(volume())).toBeUndefined();
    expect(volumeDeleteBlock(volume({ status: 'error' }))).toBeUndefined();
  });

  test('a volume a container mounts cannot be deleted', () => {
    expect(volumeDeleteBlock(volume({ used_by: [{ id: 'a', name: 'web' }] }))).toBe('inUse');
  });

  test('a volume being created or deleted cannot be deleted', () => {
    expect(volumeDeleteBlock(volume({ status: 'creating' }))).toBe('busy');
    expect(volumeDeleteBlock(volume({ status: 'deleting' }))).toBe('busy');
  });
});

describe('sortVolumes', () => {
  const failing = volume({ name: 'zeta', status: 'error', created_at: 10 });
  const busy = volume({ name: 'mid', status: 'creating', created_at: 30 });
  const big = volume({ name: 'big', used_bytes: 900, usage_observed_at: 1, created_at: 20 });
  const small = volume({ name: 'alpha', used_bytes: 5, usage_observed_at: 1, created_at: 40, used_by: [{ id: 'a', name: 'web' }, { id: 'b', name: 'db' }] });
  const names = (rows: VolumeData[]) => rows.map((row) => row.name);
  const all = [big, small, busy, failing];

  test('starts with failing volumes, then changing ones, then the rest by name', () => {
    expect(names(sortVolumes(all, defaultVolumeSort))).toEqual(['zeta', 'mid', 'alpha', 'big']);
  });

  test('sorts by name naturally and reverses', () => {
    expect(names(sortVolumes(all, { key: 'name', ascending: true }))).toEqual(['alpha', 'big', 'mid', 'zeta']);
    expect(names(sortVolumes(all, { key: 'name', ascending: false }))).toEqual(['zeta', 'mid', 'big', 'alpha']);
  });

  test('sorts by the space used, largest last when ascending', () => {
    expect(names(sortVolumes(all, { key: 'size', ascending: false }))).toEqual(['big', 'alpha', 'mid', 'zeta']);
  });

  test('sorts by how many containers use a volume', () => {
    expect(names(sortVolumes(all, { key: 'usedBy', ascending: false }))[0]).toBe('alpha');
  });

  test('sorts by age, youngest first when ascending', () => {
    expect(names(sortVolumes(all, { key: 'created', ascending: true }))).toEqual(['alpha', 'mid', 'big', 'zeta']);
  });
});

describe('matchesVolumeSearch', () => {
  const used = volume({ name: 'pg-data', used_by: [{ id: 'a', name: 'Postgres' }] });

  test('matches the name or a container using it, ignoring case', () => {
    expect(matchesVolumeSearch(used, 'PG')).toBe(true);
    expect(matchesVolumeSearch(used, 'postgres')).toBe(true);
    expect(matchesVolumeSearch(used, 'redis')).toBe(false);
  });

  test('an empty search matches every volume', () => {
    expect(matchesVolumeSearch(used, '  ')).toBe(true);
  });
});
