import { z } from 'zod';
import type { TFunction } from '@/shared/i18n';

// A port typed into a number input arrives as a string; anything but a whole
// number from 1 to 65535 is refused here instead of by the server.
// The longest subdomain prefix Core accepts.
export const maxSubdomainLength = 30;

export const isPort = (value: number) => Number.isInteger(value) && value >= 1 && value <= 65535;

// The reservation bounds Core accepts: 16 MiB to 1 TiB of memory, in whole
// MB, and 10 millicores to the largest CPU quota of 1,000,000 cores.
export const isMemoryReservationMB = (value: number) => Number.isInteger(value) && value >= 16 && value <= 1_048_576;
const isCPUReservationCores = (value: number) => value >= 0.01 && value <= 1_000_000;

// An empty optional number field arrives as '' and means "not given".
const optionalNumber = z.union([z.string(), z.number()]).optional()
  .transform(v => v === '' || v === undefined ? undefined : Number(v));

export const createContainerSchema = (t: TFunction) => z.object({
  name: z.string().min(1, t('validation.nameRequired')),
  image_tag: z.string().trim().min(1, t('validation.imageRequired')),
  // publish chooses a subdomain over a private container; while it is off,
  // the subdomain and the port are not sent.
  publish: z.boolean().optional(),
  internal_port: optionalNumber,
  domain_prefix: z.string().optional().transform(v => v === '' ? undefined : v),
  // Empty reservation fields take the platform defaults. CPU is typed in
  // cores and sent in millicores.
  memory_reservation_mb: optionalNumber
    .refine(v => v === undefined || isMemoryReservationMB(v), t('validation.memoryReservationRange')),
  cpu_reservation_cores: optionalNumber
    .refine(v => v === undefined || isCPUReservationCores(v), t('validation.cpuReservationRange'))
    .transform(v => v === undefined ? undefined : Math.round(v * 1000)),
  env_vars: z.array(z.object({
    key: z.string().min(1, t('validation.keyRequired')),
    value: z.string()
  })).optional(),
  volume_mounts: z.array(z.object({
    volume_id: z.string().min(1, t('validation.volumeRequired')),
    mount_path: z.string().min(1, t('validation.pathRequired')),
    is_readonly: z.boolean()
  })).optional()
}).superRefine((values, context) => {
  // The subdomain and port are checked only for a public container.
  if (!values.publish) return;
  const prefix = values.domain_prefix;
  if (!prefix) context.addIssue({ code: 'custom', path: ['domain_prefix'], message: t('validation.prefixRequired') });
  else if (prefix.length > maxSubdomainLength) context.addIssue({ code: 'custom', path: ['domain_prefix'], message: t('validation.max30') });
  const port = values.internal_port;
  if (port === undefined) context.addIssue({ code: 'custom', path: ['internal_port'], message: t('validation.portRequired') });
  else if (!isPort(port)) context.addIssue({ code: 'custom', path: ['internal_port'], message: t('validation.portRange') });
}).transform(({ cpu_reservation_cores, publish, domain_prefix, internal_port, ...values }) => ({
  ...values,
  domain_prefix: publish ? domain_prefix : undefined,
  internal_port: publish ? internal_port : undefined,
  cpu_reservation_millicores: cpu_reservation_cores,
}));

export type CreateContainerForm = z.input<ReturnType<typeof createContainerSchema>>;
export type CreateContainerValues = z.output<ReturnType<typeof createContainerSchema>>;

export const exposeContainerSchema = (t: TFunction) => z.object({
  domain_prefix: z.string().min(1, t('validation.prefixRequired')).max(30, t('validation.max30')),
  internal_port: z.union([z.string(), z.number()])
    .transform(v => (v === '' ? Number.NaN : Number(v)))
    .refine(isPort, t('validation.portRange')),
});
