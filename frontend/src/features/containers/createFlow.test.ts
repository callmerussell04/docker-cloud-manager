import { describe, expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import { addressPreview, imageSuggestions, memoryQuotaFit, nameFromImage, stepIssues, subdomainFrom } from './createFlow';
import type { CreateContainerForm } from './schemas';

const t = ((key: string) => key) as TFunction;

const blank: CreateContainerForm = {
  name: '',
  image_tag: '',
  publish: false,
  domain_prefix: '',
  internal_port: '',
  memory_reservation_mb: '',
  cpu_reservation_cores: '',
  env_vars: [],
  volume_mounts: [],
};

describe('stepIssues', () => {
  test('the image step asks for an image and a name', () => {
    expect(stepIssues(blank, 'image', t)).toEqual([
      { field: 'name', message: 'validation.nameRequired' },
      { field: 'image_tag', message: 'validation.imageRequired' },
    ]);
  });

  test('a step checks only its own fields', () => {
    expect(stepIssues(blank, 'publishing', t)).toEqual([]);
    expect(stepIssues(blank, 'review', t)).toEqual([]);
  });

  test('a public container needs a subdomain and a port', () => {
    expect(stepIssues({ ...blank, publish: true }, 'publishing', t)).toEqual([
      { field: 'domain_prefix', message: 'validation.prefixRequired' },
      { field: 'internal_port', message: 'validation.portRequired' },
    ]);
  });

  test('a public port keeps the schema range rule', () => {
    expect(stepIssues({ ...blank, publish: true, domain_prefix: 'shop', internal_port: '70000' }, 'publishing', t)).toEqual([
      { field: 'internal_port', message: 'validation.portRange' },
    ]);
  });

  test('a private container ignores the subdomain and port left behind', () => {
    expect(stepIssues({ ...blank, publish: false, internal_port: '70000' }, 'publishing', t)).toEqual([]);
  });

  test('the settings step names the volume mount row at fault', () => {
    const values = { ...blank, volume_mounts: [{ volume_id: 'v1', mount_path: '', is_readonly: false }] };
    expect(stepIssues(values, 'settings', t)).toEqual([
      { field: 'volume_mounts.0.mount_path', message: 'validation.pathRequired' },
    ]);
  });
});

const GiB = 1024 ** 3;
const MiB = 1024 ** 2;
const quota = { usedBytes: 2.5 * GiB, quotaBytes: 4 * GiB };

describe('memoryQuotaFit', () => {
  test('a reservation that fits leaves the rest free', () => {
    expect(memoryQuotaFit(quota, '1024')).toEqual({ heldBytes: 2.5 * GiB, addBytes: GiB, availableBytes: 1.5 * GiB, freeBytes: 0.5 * GiB, fits: true });
  });

  test('a reservation filling the quota exactly fits', () => {
    expect(memoryQuotaFit(quota, 1536).fits).toBe(true);
  });

  test('a reservation above what is left does not fit and nothing stays free', () => {
    expect(memoryQuotaFit(quota, '2048')).toEqual({ heldBytes: 2.5 * GiB, addBytes: 2 * GiB, availableBytes: 1.5 * GiB, freeBytes: 0, fits: false });
  });

  test('an empty field is the platform default, which the quota bar cannot size', () => {
    expect(memoryQuotaFit(quota, '')).toEqual({ heldBytes: 2.5 * GiB, addBytes: undefined, availableBytes: 1.5 * GiB, freeBytes: 1.5 * GiB, fits: true });
  });

  test('a figure outside the schema range is left to the range message', () => {
    expect(memoryQuotaFit(quota, '64.5').addBytes).toBeUndefined();
  });

  test('holding more than a lowered quota leaves nothing available', () => {
    const fit = memoryQuotaFit({ usedBytes: 5 * GiB, quotaBytes: 4 * GiB }, '16');
    expect(fit).toMatchObject({ availableBytes: 0, freeBytes: 0, fits: false });
  });
});

describe('stepIssues with the quota', () => {
  test('a reservation that would not fit stops the settings step', () => {
    expect(stepIssues({ ...blank, memory_reservation_mb: '2048' }, 'settings', t, quota)).toEqual([
      { field: 'memory_reservation_mb', message: 'validation.memoryOverQuota' },
    ]);
  });

  test('a reservation that fits passes', () => {
    expect(stepIssues({ ...blank, memory_reservation_mb: String(1536) }, 'settings', t, quota)).toEqual([]);
  });

  test('an unknown quota checks only the schema', () => {
    expect(stepIssues({ ...blank, memory_reservation_mb: '999999' }, 'settings', t)).toEqual([]);
  });

  test('the quota is the settings step\'s alone', () => {
    expect(stepIssues({ ...blank, name: 'web', image_tag: 'nginx', memory_reservation_mb: '2048' }, 'image', t, quota)).toEqual([]);
  });

  test('a range error is not repeated as a quota error', () => {
    expect(stepIssues({ ...blank, memory_reservation_mb: '8' }, 'settings', t, { usedBytes: 4 * GiB - 4 * MiB, quotaBytes: 4 * GiB })).toEqual([
      { field: 'memory_reservation_mb', message: 'validation.memoryReservationRange' },
    ]);
  });
});

describe('addressPreview', () => {
  const publicValues = { publish: true, domain_prefix: 'shop', internal_port: '3000', name: 'storefront' };

  test('a public container opens a port of itself on the subdomain', () => {
    expect(addressPreview(publicValues, 'example.com')).toEqual({ host: 'shop.example.com', port: 3000, name: 'storefront' });
  });

  test('the port is missing until it is typed', () => {
    expect(addressPreview({ ...publicValues, internal_port: '' }, 'example.com')).toEqual({ host: 'shop.example.com', port: undefined, name: 'storefront' });
  });

  test('a port outside the range is not previewed', () => {
    expect(addressPreview({ ...publicValues, internal_port: '70000' }, 'example.com')?.port).toBeUndefined();
  });

  test('a private container or an empty subdomain has no address', () => {
    expect(addressPreview({ ...publicValues, publish: false }, 'example.com')).toBeUndefined();
    expect(addressPreview({ ...publicValues, domain_prefix: '  ' }, 'example.com')).toBeUndefined();
  });
});

describe('nameFromImage', () => {
  test.each([
    ['nginx:1.29-alpine', 'nginx'],
    ['grafana/grafana', 'grafana'],
    ['registry.dcm.local/u-4f2a/storefront:1.8.2', 'storefront'],
    ['localhost:5000/My_App:v1', 'my-app'],
    ['nginx@sha256:0123abcd', 'nginx'],
    ['--odd..name--', 'odd-name'],
    ['', ''],
  ])('%s is named %s', (image, name) => {
    expect(nameFromImage(image)).toBe(name);
  });
});

describe('subdomainFrom', () => {
  test('a name becomes a subdomain label', () => {
    expect(subdomainFrom('My_Shop.v2')).toBe('my-shop-v2');
  });

  test('a long name is cut to the 30 characters a subdomain may have, without a trailing dash', () => {
    expect(subdomainFrom('a'.repeat(29) + '-tail')).toBe('a'.repeat(29));
  });
});

describe('imageSuggestions', () => {
  const own = ['registry.dcm.local/u-1/storefront:1.8', 'registry.dcm.local/u-1/worker:0.14'];
  const popular = ['nginx:alpine', 'postgres:17-alpine', 'node:22-alpine'];

  test('an empty field offers every image', () => {
    expect(imageSuggestions('', own, popular)).toEqual({ own, popular });
  });

  test('typing narrows both groups without regard to case', () => {
    expect(imageSuggestions('  WORK', own, popular)).toEqual({ own: ['registry.dcm.local/u-1/worker:0.14'], popular: [] });
    expect(imageSuggestions('alpine', own, popular)).toEqual({ own: [], popular: ['nginx:alpine', 'postgres:17-alpine', 'node:22-alpine'] });
  });

  test('a tag typed in full is not offered back as a suggestion', () => {
    expect(imageSuggestions('nginx:alpine', own, popular)).toEqual({ own: [], popular: [] });
  });
});
