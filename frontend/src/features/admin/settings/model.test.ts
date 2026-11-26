import { describe, expect, it } from 'vitest';

import {
  byteUnitFor,
  bytesToUnit,
  coresToMillicores,
  cpuSummary,
  defaultContainerCapacity,
  formatBytes,
  memorySummary,
  millicoresToCores,
  unitToBytes,
} from './model';

const MB = 1024 ** 2;
const GB = 1024 ** 3;

describe('byte units', () => {
  it('shows a size in the largest unit that keeps it whole', () => {
    expect(byteUnitFor(2 * GB)).toBe('GB');
    expect(byteUnitFor(256 * MB)).toBe('MB');
    expect(byteUnitFor(1536 * MB)).toBe('MB');
    expect(byteUnitFor(4096)).toBe('KB');
    expect(byteUnitFor(1000)).toBe('Bytes');
    expect(byteUnitFor(0)).toBe('MB');
  });

  it('converts to a unit and back without drift', () => {
    for (const bytes of [0, 1, 4096, 256 * MB, 1536 * MB, 10 * GB]) {
      for (const unit of ['Bytes', 'KB', 'MB', 'GB'] as const) {
        expect(unitToBytes(bytesToUnit(bytes, unit), unit)).toBe(bytes);
      }
    }
  });

  it('rounds a typed fraction to whole bytes', () => {
    expect(unitToBytes(1.5, 'GB')).toBe(1610612736);
    expect(unitToBytes(0.1, 'KB')).toBe(102);
  });

  it('formats a size for reading', () => {
    expect(formatBytes(256 * MB)).toBe('256 MB');
    expect(formatBytes(GB)).toBe('1 GB');
    expect(formatBytes(1536 * MB)).toBe('1.5 GB');
    expect(formatBytes(512)).toBe('512 Bytes');
    expect(formatBytes(0)).toBe('0 MB');
  });
});

describe('cores', () => {
  it('converts millicores to cores and back without drift', () => {
    for (const millicores of [1, 250, 333, 500, 1000, 2500, 64000]) {
      expect(coresToMillicores(millicoresToCores(millicores))).toBe(millicores);
    }
    expect(millicoresToCores(250)).toBe(0.25);
    expect(coresToMillicores(0.1 + 0.2)).toBe(300);
  });
});

describe('memory summary', () => {
  const defaults = {
    default_memory_reservation_bytes: 256 * MB,
    max_burst_multiplier: 4,
    memory_boost_multiplier: 8,
    memory_pressure_min_free_bytes: 512 * MB,
  };

  it('gives the reservation, standard limit and boost ceiling of a default container', () => {
    expect(memorySummary(defaults)).toEqual({
      reservationBytes: 256 * MB,
      standardLimitBytes: GB,
      boostCeilingBytes: 2 * GB,
      boost: 'on',
    });
  });

  it('reports boost off when the boost multiplier is not above the limit multiplier', () => {
    expect(memorySummary({ ...defaults, memory_boost_multiplier: 4 })).toMatchObject({ boost: 'off', boostCeilingBytes: GB });
    expect(memorySummary({ ...defaults, memory_boost_multiplier: 1 })).toMatchObject({ boost: 'off', boostCeilingBytes: GB });
  });

  it('reports the pressure worker off at a zero floor, whatever the multipliers', () => {
    expect(memorySummary({ ...defaults, memory_pressure_min_free_bytes: 0 })).toMatchObject({
      boost: 'pressureOff',
      boostCeilingBytes: GB,
    });
  });

  it('has no summary while a value is missing', () => {
    expect(memorySummary({ ...defaults, max_burst_multiplier: undefined })).toBeNull();
  });
});

describe('cpu summary', () => {
  const defaults = {
    default_cpu_reservation_millicores: 250,
    max_cpu_burst_multiplier: 4,
    cpu_boost_multiplier: 8,
    memory_pressure_min_free_bytes: 512 * MB,
  };

  it('gives the reservation, standard ceiling and boost ceiling of a default container in cores', () => {
    expect(cpuSummary(defaults)).toEqual({ reservationCores: 0.25, standardLimitCores: 1, boostCeilingCores: 2, boost: 'on' });
  });

  it('reports boost off at or below the limit multiplier and with the pressure worker off', () => {
    expect(cpuSummary({ ...defaults, cpu_boost_multiplier: 4 })).toMatchObject({ boost: 'off', boostCeilingCores: 1 });
    expect(cpuSummary({ ...defaults, memory_pressure_min_free_bytes: 0 })).toMatchObject({ boost: 'pressureOff', boostCeilingCores: 1 });
  });
});

describe('default container capacity', () => {
  // The defaults: 2048 MB and 2 cores, 256 MiB and 250 millicores each.
  const defaults = {
    default_user_quota_ram_mb: 2048,
    default_user_quota_cpu: 2,
    default_memory_reservation_bytes: 256 * MB,
    default_cpu_reservation_millicores: 250,
  };

  it('counts the default containers the default quota allows, limited by both when they agree', () => {
    expect(defaultContainerCapacity(defaults)).toEqual({ containers: 8, limitedBy: 'both' });
  });

  it('names the quota that runs out first', () => {
    expect(defaultContainerCapacity({ ...defaults, default_user_quota_cpu: 1 })).toEqual({ containers: 4, limitedBy: 'cpu' });
    expect(defaultContainerCapacity({ ...defaults, default_user_quota_ram_mb: 1000 })).toEqual({ containers: 3, limitedBy: 'memory' });
  });

  it('counts whole containers only', () => {
    expect(defaultContainerCapacity({ ...defaults, default_user_quota_cpu: 0.2 })).toEqual({ containers: 0, limitedBy: 'cpu' });
  });

  it('has no summary while a value is missing or a reservation is not positive', () => {
    expect(defaultContainerCapacity({ ...defaults, default_user_quota_cpu: undefined })).toBeNull();
    expect(defaultContainerCapacity({ ...defaults, default_memory_reservation_bytes: 0 })).toBeNull();
  });
});
