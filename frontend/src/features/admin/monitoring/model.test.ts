import { describe, expect, it } from 'vitest';

import { buildsQueued, appendVitalSample, vitalsHistoryLength, vitalsIntervalMs, type VitalSample, buildSlotsInUse, buildsHeldByDisk, healthChecks, cpuCapacity, diskCapacity, memoryCapacity, withDefaults } from './model';

describe('buildsHeldByDisk', () => {
  it('is true when the disk lets fewer builds run than max_concurrent_builds', () => {
    expect(buildsHeldByDisk({ ...withDefaults(), build_claim_cap: 1, max_concurrent_builds: 2 })).toBe(true);
    expect(buildsHeldByDisk({ ...withDefaults(), build_claim_cap: 0, max_concurrent_builds: 2 })).toBe(true);
  });

  it('is false when every build slot may run', () => {
    expect(buildsHeldByDisk({ ...withDefaults(), build_claim_cap: 2, max_concurrent_builds: 2 })).toBe(false);
  });

  it('is false before the first reading', () => {
    expect(buildsHeldByDisk(withDefaults())).toBe(false);
  });
});

const GB = 1024 ** 3;

describe('memoryCapacity', () => {
  const stats = {
    ...withDefaults(),
    memory_total_bytes: 32 * GB,
    dcm_reserved_memory_bytes: 16 * GB,
    dcm_reserved_build_memory_bytes: 4 * GB,
    dcm_borrowed_memory_bytes: 2 * GB,
  };

  it('splits host memory into system, containers, builds and lent, and leaves the rest free', () => {
    expect(memoryCapacity(stats, 2 * GB)).toEqual({
      total: 32 * GB,
      parts: [
        { key: 'system', value: 2 * GB },
        { key: 'containers', value: 16 * GB },
        { key: 'builds', value: 4 * GB },
        { key: 'lent', value: 2 * GB },
      ],
      free: 8 * GB,
    });
  });

  it('leaves the system part out while the system reserve is unknown', () => {
    const capacity = memoryCapacity(stats, undefined);
    expect(capacity.parts.map((part) => part.key)).toEqual(['containers', 'builds', 'lent']);
    expect(capacity.free).toBe(10 * GB);
  });

  it('has nothing free when overcommitted reservations pass the host memory', () => {
    expect(memoryCapacity({ ...stats, dcm_reserved_memory_bytes: 40 * GB }, 2 * GB).free).toBe(0);
  });
});

describe('cpuCapacity', () => {
  it('splits the host CPU into system, containers and builds, and leaves the rest free', () => {
    const stats = { ...withDefaults(), cpu_total_millicores: 16000, dcm_reserved_cpu_millicores: 9500, dcm_reserved_build_cpu_millicores: 2000 };
    expect(cpuCapacity(stats, 1000)).toEqual({
      total: 16000,
      parts: [
        { key: 'system', value: 1000 },
        { key: 'containers', value: 9500 },
        { key: 'builds', value: 2000 },
      ],
      free: 3500,
    });
  });
});

describe('diskCapacity', () => {
  it('splits the used disk into DCM data and other, and leaves the rest free', () => {
    const stats = { ...withDefaults(), disk_total_bytes: 1000 * GB, disk_used_bytes: 400 * GB, dcm_disk_used_bytes: 250 * GB };
    expect(diskCapacity(stats)).toEqual({
      total: 1000 * GB,
      parts: [
        { key: 'dcm', value: 250 * GB },
        { key: 'other', value: 150 * GB },
      ],
      free: 600 * GB,
    });
  });

  it('never counts more DCM data than the disk has used', () => {
    const stats = { ...withDefaults(), disk_total_bytes: 100, disk_used_bytes: 40, dcm_disk_used_bytes: 55 };
    expect(diskCapacity(stats).parts).toEqual([
      { key: 'dcm', value: 40 },
      { key: 'other', value: 0 },
    ]);
  });
});

describe('buildSlotsInUse', () => {
  it('counts active builds up to max_concurrent_builds, as Core reserves slots', () => {
    expect(buildSlotsInUse({ ...withDefaults(), builds_active: 1, max_concurrent_builds: 2 })).toBe(1);
    expect(buildSlotsInUse({ ...withDefaults(), builds_active: 5, max_concurrent_builds: 2 })).toBe(2);
    expect(buildSlotsInUse(withDefaults())).toBe(0);
  });
});

describe('healthChecks', () => {
  const healthy = {
    ...withDefaults(),
    admission_status: 'open',
    rootfs_quota_status: 'enforced' as const,
    volume_size_mode: 'enforced' as const,
    build_claim_cap: 2,
    max_concurrent_builds: 2,
  };

  it('marks every check good on a healthy host, in page order', () => {
    expect(healthChecks(healthy)).toEqual([
      { id: 'admission', state: 'open', mark: 'ok' },
      { id: 'containerDisk', state: 'enforced', mark: 'ok' },
      { id: 'volumeSizes', state: 'enforced', mark: 'ok' },
      { id: 'builds', state: 'free', mark: 'ok' },
    ]);
  });

  it('marks blocked admission as failing', () => {
    expect(healthChecks({ ...healthy, admission_status: 'blocked' })[0]).toEqual({ id: 'admission', state: 'blocked', mark: 'bad' });
  });

  it('warns when container disk limits or volume sizes are not enforced', () => {
    const checks = healthChecks({ ...healthy, rootfs_quota_status: 'not_enforced', volume_size_mode: 'measured' });
    expect(checks[1]).toEqual({ id: 'containerDisk', state: 'not_enforced', mark: 'warn' });
    expect(checks[2]).toEqual({ id: 'volumeSizes', state: 'measured', mark: 'warn' });
    expect(healthChecks({ ...healthy, volume_size_mode: 'unavailable' })[2].mark).toBe('warn');
  });

  it('marks an unknown container disk status as unknown, and reads an unrecognised value as unknown', () => {
    expect(healthChecks({ ...healthy, rootfs_quota_status: 'unknown' })[1]).toEqual({ id: 'containerDisk', state: 'unknown', mark: 'unknown' });
    expect(healthChecks({ ...healthy, rootfs_quota_status: 'sideways' as never })[1].state).toBe('unknown');
  });

  it('warns when the disk holds builds below max_concurrent_builds', () => {
    expect(healthChecks({ ...healthy, build_claim_cap: 1 })[3]).toEqual({ id: 'builds', state: 'heldByDisk', mark: 'warn' });
  });
});

describe('appendVitalSample', () => {
  const sample = (cpu: number): VitalSample => ({ cpu, memory: cpu * 1024 });

  it('keeps about 90 seconds of samples taken every 2 seconds', () => {
    expect(vitalsIntervalMs).toBe(2000);
    expect(vitalsHistoryLength).toBe(45);
  });

  it('starts from nothing and adds each sample after the ones before it', () => {
    const first = appendVitalSample([], sample(1));
    expect(first).toEqual([sample(1)]);
    expect(appendVitalSample(first, sample(2))).toEqual([sample(1), sample(2)]);
    expect(first).toEqual([sample(1)]);
  });

  it('drops the oldest sample once the history is full', () => {
    const full = Array.from({ length: 45 }, (_, index) => sample(index));
    const next = appendVitalSample(full, sample(999));
    expect(next).toHaveLength(45);
    expect(next[0]).toEqual(sample(1));
    expect(next[44]).toEqual(sample(999));
  });
});

describe('buildsQueued', () => {
  it('counts the active builds waiting for a slot', () => {
    expect(buildsQueued({ ...withDefaults(), builds_active: 5, max_concurrent_builds: 2 })).toBe(3);
    expect(buildsQueued({ ...withDefaults(), builds_active: 1, max_concurrent_builds: 2 })).toBe(0);
  });
});
