import { describe, expect, it } from 'vitest';

import { buildsHeldByDisk, withDefaults } from './model';

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
