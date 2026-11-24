import { describe, expect, test } from 'vitest';
import { nextUsageSort, sortUsageUsers, trendChange, type UserUsageReportItem, auditFailureKey, failureRate, rangeSeconds, readReportRange, usageSeries, usageSlotSeconds, userSeries, type UserUsagePoint } from './model';

describe('userSeries', () => {
  test('gives each user a series by the order of their ids, not by the order they arrive in', () => {
    const series = userSeries(['c', 'a', 'b']);
    expect(series.get('a')).toBe(1);
    expect(series.get('b')).toBe(2);
    expect(series.get('c')).toBe(3);
  });

  test('keeps a colour when the same users are ranked differently', () => {
    expect(userSeries(['b', 'a', 'c'])).toEqual(userSeries(['c', 'b', 'a']));
  });

  test('repeats the five series past the fifth user', () => {
    const series = userSeries(['u1', 'u2', 'u3', 'u4', 'u5', 'u6', 'u7']);
    expect([...series.values()]).toEqual([1, 2, 3, 4, 5, 1, 2]);
  });
});

function point(bucket: number, reserved: number, extra: Partial<UserUsagePoint> = {}): UserUsagePoint {
  return {
    bucket_start: bucket,
    memory_usage_bytes: 0,
    reserved_memory_bytes: reserved,
    cpu_percent: 0,
    total_disk_bytes: 0,
    resources_total: 0,
    containers_total: 0,
    containers_running: 0,
    volumes_total: 0,
    images_total: 0,
    builds_total: 0,
    projects_total: 0,
    actions_total: 0,
    ...extra,
  };
}

describe('usageSeries', () => {
  const timelines = [
    { ownerId: 'a', points: [point(0, 10), point(300, 20), point(600, 30)] },
    { ownerId: 'b', points: [point(1800, 7), point(600, 5)] },
  ];

  test('takes each user\'s latest reading in a slot, and drops slots nobody was collected in', () => {
    const series = usageSeries(timelines, 'reserved', 600);
    expect(series.times).toEqual([0, 600, 1800]);
    expect(series.values.get('a')).toEqual([20, 30, 0]);
    expect(series.values.get('b')).toEqual([0, 5, 7]);
  });

  test('totals every user in each slot, as the stacked chart\'s top', () => {
    expect(usageSeries(timelines, 'reserved', 600).totals).toEqual([20, 35, 7]);
  });

  test('reads the measure asked for', () => {
    const series = usageSeries([{ ownerId: 'a', points: [point(0, 1, { cpu_percent: 12.5, total_disk_bytes: 9, memory_usage_bytes: 4 })] }], 'cpu', 600);
    expect(series.values.get('a')).toEqual([12.5]);
    expect(usageSeries([{ ownerId: 'a', points: [point(0, 1, { total_disk_bytes: 9 })] }], 'disk', 600).values.get('a')).toEqual([9]);
    expect(usageSeries([{ ownerId: 'a', points: [point(0, 1, { memory_usage_bytes: 4 })] }], 'used', 600).values.get('a')).toEqual([4]);
  });

  test('is empty with no readings', () => {
    expect(usageSeries([{ ownerId: 'a', points: [] }], 'reserved', 600)).toEqual({ times: [], values: new Map([['a', []]]), totals: [] });
  });
});

describe('usageSlotSeconds', () => {
  test('is the collection interval while the range holds few enough of them', () => {
    expect(usageSlotSeconds(24 * 3600, 3600)).toBe(3600);
  });

  test('widens to whole intervals so a long range keeps at most 120 slots', () => {
    expect(usageSlotSeconds(24 * 3600, 300)).toBe(900);
    expect(usageSlotSeconds(30 * 24 * 3600, 300)).toBe(21600);
  });

  test('takes five minutes when the interval is unknown', () => {
    expect(usageSlotSeconds(3600, 0)).toBe(300);
  });
});

describe('auditFailureKey', () => {
  test('names a recorded error code by the API error message', () => {
    expect(auditFailureKey('quota_exceeded')).toBe('apiError.quota_exceeded');
    expect(auditFailureKey('resource_in_use')).toBe('apiError.resource_in_use');
  });

  test('names an HTTP status the audit proxy recorded by the same messages', () => {
    expect(auditFailureKey('Not Found')).toBe('apiError.not_found');
    expect(auditFailureKey('Service Unavailable')).toBe('apiError.unavailable');
    expect(auditFailureKey('Gateway Timeout')).toBe('apiError.timeout');
    expect(auditFailureKey('Too Many Requests')).toBe('admin.reports.reason.tooMany');
    expect(auditFailureKey('Request Entity Too Large')).toBe('admin.reports.reason.tooLarge');
  });

  test('says no reason was recorded, or that it is not one the page knows, never the raw code', () => {
    expect(auditFailureKey('')).toBe('admin.reports.reason.unrecorded');
    expect(auditFailureKey('something_new')).toBe('admin.reports.reason.other');
  });
});

describe('readReportRange', () => {
  test('reads 24 h, 7 days or 30 days from the URL, and 7 days for anything else', () => {
    expect(readReportRange('24h')).toBe('24h');
    expect(readReportRange('30d')).toBe('30d');
    expect(readReportRange(null)).toBe('7d');
    expect(readReportRange('90d')).toBe('7d');
  });

  test('gives each range its length', () => {
    expect(rangeSeconds['24h']).toBe(86_400);
    expect(rangeSeconds['7d']).toBe(604_800);
    expect(rangeSeconds['30d']).toBe(2_592_000);
  });
});

describe('failureRate', () => {
  test('is the share of actions that failed, in percent', () => {
    expect(failureRate(3, 200)).toBe(1.5);
  });

  test('is zero when nothing happened', () => {
    expect(failureRate(0, 0)).toBe(0);
  });
});

function user(id: string, name: string, figures: Partial<UserUsageReportItem> = {}): UserUsageReportItem {
  return {
    owner_id: id,
    owner_username: name,
    memory_usage_bytes: 0,
    reserved_memory_bytes: 0,
    cpu_percent: 0,
    total_disk_bytes: 0,
    resources_total: 0,
    containers_total: 0,
    containers_running: 0,
    volumes_total: 0,
    images_total: 0,
    builds_total: 0,
    projects_total: 0,
    actions_total: 0,
    ...figures,
  };
}

describe('sortUsageUsers', () => {
  const users = [
    user('1', 'carol', { reserved_memory_bytes: 300, cpu_percent: 2, actions_total: 9 }),
    user('2', 'alice', { reserved_memory_bytes: 100, cpu_percent: 7, actions_total: 9 }),
    user('3', 'Bob', { reserved_memory_bytes: 300, cpu_percent: 1, actions_total: 1 }),
  ];
  const names = (rows: UserUsageReportItem[]) => rows.map((row) => row.owner_username);

  test('ranks by memory reserved, largest first, by default, equal figures by name', () => {
    expect(names(sortUsageUsers(users, { key: 'reserved', ascending: false }, new Map()))).toEqual(['Bob', 'carol', 'alice']);
  });

  test('sorts by name without case', () => {
    expect(names(sortUsageUsers(users, { key: 'user', ascending: true }, new Map()))).toEqual(['alice', 'Bob', 'carol']);
  });

  test('sorts by any figure, either way', () => {
    expect(names(sortUsageUsers(users, { key: 'cpu', ascending: true }, new Map()))).toEqual(['Bob', 'carol', 'alice']);
    expect(names(sortUsageUsers(users, { key: 'actions', ascending: false }, new Map()))).toEqual(['alice', 'carol', 'Bob']);
  });

  test('sorts the trend by how much each user grew over the range', () => {
    const trends = new Map([['1', -50], ['2', 200], ['3', 0]]);
    expect(names(sortUsageUsers(users, { key: 'trend', ascending: false }, trends))).toEqual(['alice', 'Bob', 'carol']);
  });
});

describe('trendChange', () => {
  test('is the last figure less the first', () => {
    expect(trendChange([100, 400, 250])).toBe(150);
  });

  test('is zero without two figures', () => {
    expect(trendChange([7])).toBe(0);
  });
});

describe('nextUsageSort', () => {
  test('ranks a newly chosen figure largest first', () => {
    expect(nextUsageSort({ key: 'reserved', ascending: false }, 'cpu')).toEqual({ key: 'cpu', ascending: false });
  });

  test('sorts newly chosen names from A', () => {
    expect(nextUsageSort({ key: 'reserved', ascending: false }, 'user')).toEqual({ key: 'user', ascending: true });
  });

  test('reverses the column already chosen', () => {
    expect(nextUsageSort({ key: 'cpu', ascending: false }, 'cpu')).toEqual({ key: 'cpu', ascending: true });
  });
});
