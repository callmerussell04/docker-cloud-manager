import { describe, expect, test } from 'vitest';
import { blockedContainerActions, holdsMemoryReservation, borrowedMemoryBytes, containerBadgeVariant, containerFilterCounts, containerNeedsPolling, containerNeedsWatching, containerTone, defaultContainerSort, filterContainers, sortContainers, formatCores, isContainerBusy, appendUsageSample, consoleViewOf, consoleViewSearch, containerPagePath, usageHistoryLength, type ContainerData, type UsageSample } from './model';

function container(status: string, ttlDeadline?: number): ContainerData {
  return { id: 'c1', name: 'web', image_tag: 'nginx', internal_port: 80, domain_prefix: '', status, last_error: '', ttl_deadline: ttlDeadline, created_at: 0, memory_reservation_bytes: 256 * 1024 ** 2, cpu_reservation_millicores: 250 };
}

describe('containerBadgeVariant', () => {
  test.each([
    ['running', 'success'],
    ['pending', 'info'],
    ['created', 'default'],
    ['creating', 'warning'],
    ['starting', 'info'],
    ['stopping', 'warning'],
    ['exposing', 'info'],
    ['exited', 'default'],
    ['deleting', 'warning'],
    ['missing', 'error'],
    ['reconciling', 'warning'],
    ['error', 'error'],
    ['something-new', 'info'],
  ])('%s shows a %s badge', (status, variant) => {
    expect(containerBadgeVariant(status)).toBe(variant);
  });
});

describe('isContainerBusy', () => {
  test.each([
    ['pending', true],
    ['creating', true],
    ['starting', true],
    ['stopping', true],
    ['exposing', true],
    ['deleting', true],
    ['missing', true],
    ['reconciling', true],
    ['running', false],
    ['created', false],
    ['exited', false],
    ['error', false],
  ])('%s busy: %s', (status, busy) => {
    expect(isContainerBusy(status)).toBe(busy);
  });
});

describe('containerNeedsPolling', () => {
  test('a busy container is polled until it settles', () => {
    expect(containerNeedsPolling(container('reconciling'))).toBe(true);
    expect(containerNeedsPolling(container('missing'))).toBe(true);
  });

  test('a running container with an auto-stop deadline is polled', () => {
    expect(containerNeedsPolling(container('running', 1_700_000_000))).toBe(true);
  });

  test('a settled container without a deadline is not polled', () => {
    expect(containerNeedsPolling(container('running'))).toBe(false);
    expect(containerNeedsPolling(container('exited', 1_700_000_000))).toBe(false);
  });
});

describe('blockedContainerActions', () => {
  // Columns: start, stop, expose, delete, terminal (true = blocked).
  test.each([
    ['running', true, false, false, false, false],
    ['exited', false, true, false, false, true],
    ['created', false, true, false, false, true],
    ['error', false, true, false, false, true],
    ['pending', true, true, true, false, true],
    ['creating', true, true, true, false, true],
    ['starting', true, true, true, false, true],
    ['stopping', true, true, true, false, true],
    ['exposing', true, true, true, true, true],
    ['deleting', true, true, true, true, true],
    ['missing', true, true, true, false, true],
    ['reconciling', true, true, true, false, true],
  ])('%s blocks start %s, stop %s, expose %s, delete %s, terminal %s', (status, start, stop, expose, del, terminal) => {
    expect(blockedContainerActions(status)).toEqual({ start, stop, expose, delete: del, terminal });
  });
});

describe('borrowedMemoryBytes', () => {
  const MiB = 1024 * 1024;
  const stats = (limit: number, standard: number) => ({
    cpu_percentage: 0,
    memory_usage_bytes: 0,
    memory_limit_bytes: limit,
    memory_standard_limit_bytes: standard,
    network_rx_bytes: 0,
    network_tx_bytes: 0,
  });

  test('is how far the current limit is above the standard limit', () => {
    expect(borrowedMemoryBytes(stats(768 * MiB, 512 * MiB))).toBe(256 * MiB);
  });

  test('is zero at or below the standard limit', () => {
    expect(borrowedMemoryBytes(stats(512 * MiB, 512 * MiB))).toBe(0);
    expect(borrowedMemoryBytes(stats(256 * MiB, 512 * MiB))).toBe(0);
  });

  test('is zero when the standard limit is unknown', () => {
    expect(borrowedMemoryBytes(stats(768 * MiB, 0))).toBe(0);
    expect(borrowedMemoryBytes(undefined)).toBe(0);
  });
});

describe('containerNeedsWatching', () => {
  test.each([
    ['running', true],
    ['starting', true],
    ['deleting', true],
    ['reconciling', true],
    ['exited', false],
    ['created', false],
    ['error', false],
  ])('%s -> %s', (status, expected) => {
    expect(containerNeedsWatching(container(status))).toBe(expected);
  });
});

describe('formatCores', () => {
  test.each([
    [250, '0.25'],
    [2000, '2'],
    [10, '0.01'],
    [1500, '1.5'],
  ])('%i millicores show as %s cores', (millicores, cores) => {
    expect(formatCores(millicores)).toBe(cores);
  });
});

describe('containerTone', () => {
  test.each([
    ['running', 'ok'],
    ['error', 'bad'],
    ['missing', 'bad'],
    ['exited', 'idle'],
    ['created', 'idle'],
    ['pending', 'busy'],
    ['creating', 'busy'],
    ['starting', 'busy'],
    ['stopping', 'busy'],
    ['exposing', 'busy'],
    ['deleting', 'busy'],
    ['reconciling', 'busy'],
    ['something-new', 'busy'],
  ])('%s is %s', (status, tone) => {
    expect(containerTone(status)).toBe(tone);
  });
});

describe('the status filter', () => {
  const rows = [
    { ...container('running'), id: 'run-1' },
    { ...container('running'), id: 'run-2' },
    { ...container('exited'), id: 'exited' },
    { ...container('created'), id: 'created' },
    { ...container('error'), id: 'error' },
    { ...container('missing'), id: 'missing' },
    { ...container('starting'), id: 'starting' },
  ];
  const ids = (list: ContainerData[]) => list.map((item) => item.id);

  test('All keeps every container', () => {
    expect(ids(filterContainers(rows, 'all'))).toEqual(['run-1', 'run-2', 'exited', 'created', 'error', 'missing', 'starting']);
  });

  test('Running keeps running containers', () => {
    expect(ids(filterContainers(rows, 'running'))).toEqual(['run-1', 'run-2']);
  });

  test('Stopped keeps exited and created containers', () => {
    expect(ids(filterContainers(rows, 'stopped'))).toEqual(['exited', 'created']);
  });

  test('Attention keeps failing and busy containers', () => {
    expect(ids(filterContainers(rows, 'attention'))).toEqual(['error', 'missing', 'starting']);
  });

  test('counts each filter over the whole list', () => {
    expect(containerFilterCounts(rows)).toEqual({ all: 7, running: 2, stopped: 2, attention: 3 });
  });

  test('an empty list counts zero everywhere', () => {
    expect(containerFilterCounts([])).toEqual({ all: 0, running: 0, stopped: 0, attention: 0 });
  });
});

describe('sortContainers', () => {
  const MiB = 1024 * 1024;
  const row = (id: string, status: string, extra: Partial<ContainerData> = {}): ContainerData => ({ ...container(status), id, name: id, ...extra });
  const ids = (list: ContainerData[]) => list.map((item) => item.id);

  test('starts with failing containers, then changing, running and at rest', () => {
    const rows = [row('exited', 'exited'), row('running', 'running'), row('starting', 'starting'), row('error', 'error'), row('created', 'created'), row('missing', 'missing')];
    expect(defaultContainerSort).toEqual({ key: 'status', ascending: true });
    expect(ids(sortContainers(rows, defaultContainerSort))).toEqual(['error', 'missing', 'starting', 'running', 'created', 'exited']);
  });

  test('orders containers with the same status by name, whichever way status sorts', () => {
    const rows = [row('b', 'running'), row('c', 'error'), row('a', 'running')];
    expect(ids(sortContainers(rows, { key: 'status', ascending: true }))).toEqual(['c', 'a', 'b']);
    expect(ids(sortContainers(rows, { key: 'status', ascending: false }))).toEqual(['a', 'b', 'c']);
  });

  test('sorts by name', () => {
    const rows = [row('web-10', 'running'), row('api', 'running'), row('web-2', 'running')];
    expect(ids(sortContainers(rows, { key: 'name', ascending: true }))).toEqual(['api', 'web-2', 'web-10']);
  });

  test('sorts by address, with unpublished containers last', () => {
    const rows = [row('none', 'running'), row('shop', 'running', { domain_prefix: 'shop' }), row('api', 'running', { domain_prefix: 'api' })];
    expect(ids(sortContainers(rows, { key: 'address', ascending: true }))).toEqual(['api', 'shop', 'none']);
  });

  test('sorts by memory reservation', () => {
    const rows = [row('mid', 'running', { memory_reservation_bytes: 512 * MiB }), row('small', 'running', { memory_reservation_bytes: 128 * MiB }), row('big', 'running', { memory_reservation_bytes: 1024 * MiB })];
    expect(ids(sortContainers(rows, { key: 'memory', ascending: false }))).toEqual(['big', 'mid', 'small']);
  });

  test('sorts by creation, newest first when ascending, as the age column reads', () => {
    const rows = [row('old', 'running', { created_at: 100 }), row('new', 'running', { created_at: 300 }), row('mid', 'running', { created_at: 200 })];
    expect(ids(sortContainers(rows, { key: 'created', ascending: true }))).toEqual(['new', 'mid', 'old']);
  });
});

describe('appendUsageSample', () => {
  const sample = (cpu: number): UsageSample => ({ cpu, memory: cpu * 1024 });

  test('starts a history from nothing with the first sample', () => {
    expect(appendUsageSample([], sample(4))).toEqual([sample(4)]);
  });

  test('adds a sample after the ones before it without changing them', () => {
    const before = [sample(1), sample(2)];
    expect(appendUsageSample(before, sample(3))).toEqual([sample(1), sample(2), sample(3)]);
    expect(before).toEqual([sample(1), sample(2)]);
  });

  test('drops the oldest sample once the history is full', () => {
    const full = Array.from({ length: usageHistoryLength }, (_, index) => sample(index));
    const next = appendUsageSample(full, sample(999));
    expect(next).toHaveLength(usageHistoryLength);
    expect(next[0]).toEqual(sample(1));
    expect(next.at(-1)).toEqual(sample(999));
  });
});

describe('consoleViewOf', () => {
  test('opens the logs unless the link asks for the terminal', () => {
    expect(consoleViewOf(new URLSearchParams(''))).toBe('logs');
    expect(consoleViewOf(new URLSearchParams('view=logs'))).toBe('logs');
    expect(consoleViewOf(new URLSearchParams('view=terminal'))).toBe('terminal');
    expect(consoleViewOf(new URLSearchParams('view=shell'))).toBe('logs');
  });

  test('links to the terminal with ?view=terminal and to the logs with nothing', () => {
    expect(consoleViewSearch('terminal')).toBe('?view=terminal');
    expect(consoleViewSearch('logs')).toBe('');
    expect(consoleViewOf(new URLSearchParams(consoleViewSearch('terminal')))).toBe('terminal');
  });

  test('links to a container page under its list, on the logs unless asked', () => {
    expect(containerPagePath('/containers', 'c1')).toBe('/containers/c1');
    expect(containerPagePath('/admin/containers', 'c1', 'terminal')).toBe('/admin/containers/c1?view=terminal');
  });
});

describe('holdsMemoryReservation', () => {
  test('a running container, or one on its way to running, holds its reservation', () => {
    expect(holdsMemoryReservation(container('running'))).toBe(true);
    expect(holdsMemoryReservation({ ...container('starting'), desired_status: 'running' })).toBe(true);
  });

  test('a stopped, stopping or missing container holds none', () => {
    expect(holdsMemoryReservation(container('exited'))).toBe(false);
    expect(holdsMemoryReservation({ ...container('stopping'), desired_status: 'exited' })).toBe(false);
    expect(holdsMemoryReservation({ ...container('missing'), desired_status: 'running' })).toBe(false);
  });
});
