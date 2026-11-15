import type { ContainerStats } from './model';

// borrowedMemoryBytes is how far a container's current memory limit is above
// its standard limit: memory lent from idle host capacity that the platform
// may reclaim. It is 0 when the standard limit is unknown, as it is for a
// container that is not running.
export function borrowedMemoryBytes(stats?: ContainerStats): number {
  const standard = stats?.memory_standard_limit_bytes ?? 0;
  if (standard <= 0) {
    return 0;
  }
  return Math.max((stats?.memory_limit_bytes ?? 0) - standard, 0);
}
