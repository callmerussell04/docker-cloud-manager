import { describe, expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import { createContainerSchema } from './schemas';

const t = ((key: string) => key) as TFunction;
const base = { name: 'web', image_tag: 'nginx:latest' };

describe('createContainerSchema reservations', () => {
  test('empty fields leave the platform defaults', () => {
    const parsed = createContainerSchema(t).parse({ ...base, memory_reservation_mb: '', cpu_reservation_cores: '' });
    expect(parsed.memory_reservation_mb).toBeUndefined();
    expect(parsed.cpu_reservation_millicores).toBeUndefined();
  });

  test('memory is sent in MB and CPU in millicores', () => {
    const parsed = createContainerSchema(t).parse({ ...base, memory_reservation_mb: '512', cpu_reservation_cores: '0.25' });
    expect(parsed.memory_reservation_mb).toBe(512);
    expect(parsed.cpu_reservation_millicores).toBe(250);
  });

  test('the smallest and largest figures are accepted', () => {
    expect(createContainerSchema(t).safeParse({ ...base, memory_reservation_mb: 16, cpu_reservation_cores: 0.01 }).success).toBe(true);
    expect(createContainerSchema(t).safeParse({ ...base, memory_reservation_mb: 1_048_576, cpu_reservation_cores: 1_000_000 }).success).toBe(true);
  });

  test.each([
    ['memory_reservation_mb', '15', 'validation.memoryReservationRange'],
    ['memory_reservation_mb', '1048577', 'validation.memoryReservationRange'],
    ['memory_reservation_mb', '64.5', 'validation.memoryReservationRange'],
    ['memory_reservation_mb', 'abc', 'validation.memoryReservationRange'],
    ['cpu_reservation_cores', '0.009', 'validation.cpuReservationRange'],
    ['cpu_reservation_cores', '0.0095', 'validation.cpuReservationRange'],
    ['cpu_reservation_cores', '1000001', 'validation.cpuReservationRange'],
    ['cpu_reservation_cores', '-1', 'validation.cpuReservationRange'],
  ])('%s %s is refused', (field, value, message) => {
    const result = createContainerSchema(t).safeParse({ ...base, [field]: value });
    expect(result.success).toBe(false);
    expect(result.error?.issues.map(issue => issue.message)).toContain(message);
  });
});

describe('createContainerSchema publishing', () => {
  test('a private container sends no subdomain or port, whatever was typed', () => {
    const parsed = createContainerSchema(t).parse({ ...base, publish: false, domain_prefix: 'shop', internal_port: '3000' });
    expect(parsed.domain_prefix).toBeUndefined();
    expect(parsed.internal_port).toBeUndefined();
  });

  test('a public container sends its subdomain and port', () => {
    const parsed = createContainerSchema(t).parse({ ...base, publish: true, domain_prefix: 'shop', internal_port: '3000' });
    expect(parsed.domain_prefix).toBe('shop');
    expect(parsed.internal_port).toBe(3000);
  });

  test('a public subdomain keeps the 30 character limit', () => {
    const result = createContainerSchema(t).safeParse({ ...base, publish: true, domain_prefix: 'a'.repeat(31), internal_port: '80' });
    expect(result.error?.issues.map((issue) => issue.message)).toEqual(['validation.max30']);
  });
});
