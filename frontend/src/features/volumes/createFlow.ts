import type { TFunction } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { MIB } from './model';
import { createVolumeSchema, isSizeMiB, type CreateVolumeForm } from './schemas';

// The create volume dialog's steps, in order.
export const createVolumeSteps = ['volume', 'review'] as const;
export type CreateVolumeStep = (typeof createVolumeSteps)[number];

// The form fields each step shows; Continue checks only these.
const stepFields: Record<CreateVolumeStep, readonly (keyof CreateVolumeForm)[]> = {
  volume: ['name', 'size_mib'],
  review: [],
};

// DiskQuota is the user's disk quota and what their images and volume sizes
// already reserve against it (the dashboard's disk_quota_mb and
// disk_used_mb, in bytes).
export interface DiskQuota {
  usedBytes: number;
  quotaBytes: number;
}

export interface DiskQuotaFit {
  // heldBytes is what the user's images and volumes already reserve.
  heldBytes: number;
  // addBytes is the new volume's size; undefined for the default size, which
  // users cannot see, or a figure the schema refuses.
  addBytes: number | undefined;
  // availableBytes is what the quota has left before this volume.
  availableBytes: number;
  // freeBytes is what stays free after it.
  freeBytes: number;
  fits: boolean;
}

// typedSizeBytes is a typed size in bytes; undefined while it is empty or not
// a whole number of MiB.
export function typedSizeBytes(sizeMiB: string | undefined) {
  return sizeMiB && isSizeMiB(sizeMiB) ? Number(sizeMiB.trim()) * MIB : undefined;
}

// diskQuotaFit places a volume size, typed in MiB, against the disk quota
// left. An empty or refused figure adds nothing the bar can size and is left
// to Core and the schema's message.
export function diskQuotaFit(quota: DiskQuota, sizeMiB: string | undefined): DiskQuotaFit {
  const addBytes = typedSizeBytes(sizeMiB);
  const availableBytes = Math.max(quota.quotaBytes - quota.usedBytes, 0);
  const fits = addBytes === undefined || addBytes <= availableBytes;
  return {
    heldBytes: quota.usedBytes,
    addBytes,
    availableBytes,
    freeBytes: fits ? availableBytes - (addBytes ?? 0) : 0,
    fits,
  };
}

export interface VolumeStepIssue {
  field: keyof CreateVolumeForm;
  message: string;
}

// volumeStepIssues is what keeps a step from moving on: the schema's messages
// for the fields the step shows, and on the volume step a size the disk quota
// left cannot hold, when the quota is known.
export function volumeStepIssues(values: CreateVolumeForm, step: CreateVolumeStep, t: TFunction, quota?: DiskQuota): VolumeStepIssue[] {
  const result = createVolumeSchema(t).safeParse(values);
  const fields = stepFields[step] as readonly PropertyKey[];
  const issues = result.success ? [] : result.error.issues
    .filter((issue) => fields.includes(issue.path[0]))
    .map((issue) => ({ field: issue.path[0] as keyof CreateVolumeForm, message: issue.message }));
  if (step === 'volume' && quota) {
    const fit = diskQuotaFit(quota, values.size_mib);
    if (!fit.fits) issues.push({ field: 'size_mib', message: t('validation.diskOverQuota', { free: formatBytes(fit.availableBytes, 1) }) });
  }
  return issues;
}
