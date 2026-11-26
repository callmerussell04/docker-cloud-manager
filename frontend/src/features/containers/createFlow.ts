import type { TFunction } from '@/shared/i18n';
import { formatBytes } from '@/shared/lib/utils';
import { createContainerSchema, isMemoryReservationMB, isPort, maxSubdomainLength, type CreateContainerForm } from './schemas';

// The create dialog's steps, in order.
export const createSteps = ['image', 'publishing', 'settings', 'review'] as const;
export type CreateStep = (typeof createSteps)[number];

// The form fields each step shows; Continue checks only these.
const stepFields: Record<CreateStep, readonly (keyof CreateContainerForm)[]> = {
  image: ['name', 'image_tag'],
  publishing: ['publish', 'domain_prefix', 'internal_port'],
  settings: ['memory_reservation_mb', 'cpu_reservation_cores', 'env_vars', 'volume_mounts'],
  review: [],
};

export interface StepIssue {
  // field is the form path the message belongs under, such as
  // volume_mounts.0.mount_path.
  field: string;
  message: string;
}

// MemoryQuota is the user's memory quota and what their containers already
// reserve against it (the dashboard's ram_quota_bytes and ram_used_bytes).
export interface MemoryQuota {
  usedBytes: number;
  quotaBytes: number;
}

export interface MemoryQuotaFit {
  // heldBytes is what the user's containers already reserve.
  heldBytes: number;
  // addBytes is what this container reserves; undefined for the platform
  // default, whose size the user cannot see, or a figure the schema refuses.
  addBytes: number | undefined;
  // availableBytes is what the quota has left before this container.
  availableBytes: number;
  // freeBytes is what stays free after it.
  freeBytes: number;
  fits: boolean;
}

export const MiB = 1024 * 1024;

// typedFigure reads a number field as typed: undefined while it is empty, NaN
// for text that is not a number.
export function typedFigure(value: string | number | undefined) {
  return value === '' || value === undefined ? undefined : Number(value);
}

// memoryQuotaFit places a memory reservation, typed in MB, against the quota
// left. An empty or refused figure adds nothing the bar can size and is left
// to Core and the range message.
export function memoryQuotaFit(quota: MemoryQuota, memoryMB: string | number | undefined): MemoryQuotaFit {
  const typed = typedFigure(memoryMB);
  const addBytes = typed !== undefined && isMemoryReservationMB(typed) ? typed * MiB : undefined;
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

// stepIssues is what keeps a step from moving on: the create-container
// schema's messages for the fields the step shows, and on the settings step a
// memory reservation the quota left cannot hold, when the quota is known.
export function stepIssues(values: CreateContainerForm, step: CreateStep, t: TFunction, quota?: MemoryQuota): StepIssue[] {
  const result = createContainerSchema(t).safeParse(values);
  const fields = stepFields[step] as readonly PropertyKey[];
  const issues = result.success ? [] : result.error.issues
    .filter((issue) => fields.includes(issue.path[0]))
    .map((issue) => ({ field: issue.path.join('.'), message: issue.message }));
  if (step === 'settings' && quota) {
    const fit = memoryQuotaFit(quota, values.memory_reservation_mb);
    if (!fit.fits) issues.push({ field: 'memory_reservation_mb', message: t('validation.memoryOverQuota', { free: formatBytes(fit.availableBytes, 1) }) });
  }
  return issues;
}

export interface AddressPreview {
  // host is the published address, such as shop.example.com.
  host: string;
  // port is the container port it opens; undefined until a valid one is typed.
  port: number | undefined;
  name: string;
}

// addressPreview is what a public container will publish: its subdomain on
// the base domain and the port of the container it opens. A private container
// or an empty subdomain publishes nothing.
export function addressPreview(
  values: Pick<CreateContainerForm, 'publish' | 'domain_prefix' | 'internal_port' | 'name'>,
  baseDomain: string,
): AddressPreview | undefined {
  const subdomain = values.domain_prefix?.trim();
  if (!values.publish || !subdomain) return undefined;
  const port = typedFigure(values.internal_port);
  return { host: `${subdomain}.${baseDomain}`, port: port !== undefined && isPort(port) ? port : undefined, name: values.name };
}

// slug keeps lowercase letters, digits and single dashes between them.
function slug(text: string) {
  return text.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
}

// nameFromImage suggests a container name from an image reference: its last
// path part, without the tag or digest.
export function nameFromImage(image: string) {
  const last = image.trim().split('/').at(-1) ?? '';
  return slug(last.split('@')[0].split(':')[0]);
}

// subdomainFrom suggests a subdomain from a container name.
export function subdomainFrom(name: string) {
  return slug(name).slice(0, maxSubdomainLength).replace(/-+$/, '');
}

// popularImages are the Docker Hub images the image field offers beside the
// user's own; any other tag can be typed.
export const popularImages = [
  'nginx:alpine',
  'httpd:alpine',
  'node:22-alpine',
  'python:3.13-slim',
  'postgres:17-alpine',
  'mysql:8.4',
  'redis:7-alpine',
  'mongo:8',
  'grafana/grafana:latest',
] as const;

export interface ImageSuggestions {
  own: string[];
  popular: string[];
}

// imageSuggestions narrows the image field's options to those containing what
// is typed, ignoring case. A tag typed in full is already the field's value
// and is not offered back.
export function imageSuggestions(query: string, own: readonly string[], popular: readonly string[]): ImageSuggestions {
  const typed = query.trim().toLowerCase();
  const offered = (image: string) => {
    const name = image.toLowerCase();
    return name.includes(typed) && name !== typed;
  };
  return { own: own.filter(offered), popular: popular.filter(offered) };
}
