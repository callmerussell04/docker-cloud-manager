import { useWatch, type Control } from 'react-hook-form';
import { Info } from 'lucide-react';

import type { TFunction } from '@/shared/i18n';
import { cpuSummary, defaultContainerCapacity, formatBytes, memorySummary, type SystemConfig } from '../model';

function SummaryNote({ lines }: { lines: string[] }) {
  return (
    <div className="flex items-start gap-3 rounded-xl border border-separator bg-fill p-3 text-sm text-label-2">
      <Info className="mt-0.5 h-4 w-4 shrink-0" />
      <p className="leading-relaxed">{lines.join(' ')}</p>
    </div>
  );
}

/** What the memory settings in the form give a default container, before they are saved. */
export function MemorySummaryNote({ control, t }: { control: Control<SystemConfig>; t: TFunction }) {
  const [reservation, limit, boost, floor] = useWatch({
    control,
    name: ['default_memory_reservation_bytes', 'max_burst_multiplier', 'memory_boost_multiplier', 'memory_pressure_min_free_bytes'],
  });
  const summary = memorySummary({
    default_memory_reservation_bytes: reservation,
    max_burst_multiplier: limit,
    memory_boost_multiplier: boost,
    memory_pressure_min_free_bytes: floor,
  });
  if (!summary) return null;

  return (
    <SummaryNote
      lines={[
        t('admin.settings.summary.memory.base', {
          reservation: formatBytes(summary.reservationBytes),
          limit: formatBytes(summary.standardLimitBytes),
        }),
        t(`admin.settings.summary.memory.${summary.boost}`, { ceiling: formatBytes(summary.boostCeilingBytes) }),
      ]}
    />
  );
}

/** The same for CPU, in cores. */
export function CPUSummaryNote({ control, t }: { control: Control<SystemConfig>; t: TFunction }) {
  const [reservation, limit, boost, floor] = useWatch({
    control,
    name: ['default_cpu_reservation_millicores', 'max_cpu_burst_multiplier', 'cpu_boost_multiplier', 'memory_pressure_min_free_bytes'],
  });
  const summary = cpuSummary({
    default_cpu_reservation_millicores: reservation,
    max_cpu_burst_multiplier: limit,
    cpu_boost_multiplier: boost,
    memory_pressure_min_free_bytes: floor,
  });
  if (!summary) return null;
  const cores = (value: number) => `${value} ${t('admin.settings.unit.cores')}`;

  return (
    <SummaryNote
      lines={[
        t('admin.settings.summary.cpu.base', {
          reservation: summary.reservationCores,
          limit: cores(summary.standardLimitCores),
        }),
        t(`admin.settings.summary.cpu.${summary.boost}`, { ceiling: cores(summary.boostCeilingCores) }),
      ]}
    />
  );
}

/** How many containers with the default reservations the default quota allows, and which quota limits them. */
export function QuotaSummaryNote({ control, t }: { control: Control<SystemConfig>; t: TFunction }) {
  const [quotaMB, quotaCores, memoryReservation, cpuReservation] = useWatch({
    control,
    name: ['default_user_quota_ram_mb', 'default_user_quota_cpu', 'default_memory_reservation_bytes', 'default_cpu_reservation_millicores'],
  });
  const capacity = defaultContainerCapacity({
    default_user_quota_ram_mb: quotaMB,
    default_user_quota_cpu: quotaCores,
    default_memory_reservation_bytes: memoryReservation,
    default_cpu_reservation_millicores: cpuReservation,
  });
  if (!capacity) return null;

  return (
    <SummaryNote
      lines={[t(`admin.settings.summary.quota.${capacity.limitedBy}`, { containers: capacity.containers })]}
    />
  );
}
