import { describe, expect, test } from 'vitest';
import { blockedContainerActions, borrowedMemoryBytes, containerBadgeVariant, containerNeedsPolling, containerNeedsWatching, isContainerBusy, type ContainerData } from './model';

function container(status: string, ttlDeadline?: number): ContainerData {
  return { id: 'c1', name: 'web', image_tag: 'nginx', internal_port: 80, domain_prefix: '', status, last_error: '', ttl_deadline: ttlDeadline, created_at: 0 };
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
