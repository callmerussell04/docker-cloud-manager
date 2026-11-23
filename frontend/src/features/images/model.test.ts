import { describe, expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import {
  defaultImageSort,
  imageDeleteBlock,
  imageNameLines,
  matchesImageSearch,
  sortImages,
  imageTone,
  imageUsersLabel,
  isImageBusy,
  type ImageData,
} from './model';

const t = ((key: string, params?: Record<string, string>) => (params ? `${key} ${JSON.stringify(params)}` : key)) as TFunction;

function image(overrides: Partial<ImageData> = {}): ImageData {
  return {
    id: overrides.tag ?? 'id',
    tag: 'shop:v1',
    size_bytes: 100 * 1024 ** 2,
    status: 'available',
    last_error: '',
    created_at: 1_700_000_000,
    used_by: [],
    ...overrides,
  };
}

test.each([
  ['building', true],
  ['deleting', true],
  ['available', false],
  ['missing', false],
  ['error', false],
])('isImageBusy(%s) -> %s', (status, expected) => {
  expect(isImageBusy(status)).toBe(expected);
});

test.each([
  ['available', 'ok'],
  ['error', 'bad'],
  ['missing', 'bad'],
  ['building', 'busy'],
  ['deleting', 'busy'],
  ['something-new', 'busy'],
])('imageTone(%s) -> %s', (status, tone) => {
  expect(imageTone(status)).toBe(tone);
});

describe('imageDeleteBlock', () => {
  test('a container created from the image blocks its delete, as Core refuses it', () => {
    expect(imageDeleteBlock(image({ used_by: [{ id: 'c1', name: 'web' }] }))).toBe('inUse');
  });

  test('an image still building or already being deleted cannot be deleted', () => {
    expect(imageDeleteBlock(image({ status: 'building' }))).toBe('busy');
    expect(imageDeleteBlock(image({ status: 'deleting' }))).toBe('busy');
  });

  test('a failed image cannot be deleted, for its own reason', () => {
    expect(imageDeleteBlock(image({ status: 'error' }))).toBe('failed');
  });

  test('an unused available or missing image can be deleted', () => {
    expect(imageDeleteBlock(image())).toBeUndefined();
    expect(imageDeleteBlock(image({ status: 'missing' }))).toBeUndefined();
  });
});

describe('imageUsersLabel', () => {
  test('names up to two containers, then how many more', () => {
    expect(imageUsersLabel(t, [])).toBeUndefined();
    expect(imageUsersLabel(t, [{ id: '1', name: 'web' }, { id: '2', name: 'api' }])).toBe('web, api');
    expect(imageUsersLabel(t, [{ id: '1', name: 'web' }, { id: '2', name: 'api' }, { id: '3', name: 'jobs' }])).toBe(
      'images.usedByMore {"names":"web, api","count":1}',
    );
  });
});

describe('imageNameLines', () => {
  test('a tag with a registry path shows its repository and tag, the path beneath', () => {
    expect(imageNameLines('registry.example.com/team/shop:1.8.2')).toEqual({ name: 'shop:1.8.2', path: 'registry.example.com/team' });
  });

  test('a short tag is its own name', () => {
    expect(imageNameLines('shop:v1')).toEqual({ name: 'shop:v1', path: undefined });
  });
});

describe('matchesImageSearch', () => {
  test('matches the tag or a container using the image, ignoring case', () => {
    const used = image({ tag: 'Shop:v1', used_by: [{ id: 'c1', name: 'storefront' }] });
    expect(matchesImageSearch(used, 'shop')).toBe(true);
    expect(matchesImageSearch(used, 'STORE')).toBe(true);
    expect(matchesImageSearch(used, 'worker')).toBe(false);
    expect(matchesImageSearch(used, '  ')).toBe(true);
  });
});

describe('sortImages', () => {
  const failed = image({ tag: 'collector:0.7', status: 'error' });
  const building = image({ tag: 'landing:2', status: 'building' });
  const ready = image({ tag: 'api:1', size_bytes: 10, created_at: 100, used_by: [{ id: 'c', name: 'api' }] });
  const old = image({ tag: 'web:1', size_bytes: 500, created_at: 50 });

  test('starts with failing images, then changing ones, then ready ones by name', () => {
    expect(sortImages([old, ready, building, failed], defaultImageSort).map((i) => i.tag)).toEqual([
      'collector:0.7', 'landing:2', 'api:1', 'web:1',
    ]);
  });

  test('sorts by size, use and age', () => {
    expect(sortImages([ready, old], { key: 'size', ascending: false }).map((i) => i.tag)).toEqual(['web:1', 'api:1']);
    expect(sortImages([old, ready], { key: 'usedBy', ascending: false }).map((i) => i.tag)).toEqual(['api:1', 'web:1']);
    expect(sortImages([old, ready], { key: 'created', ascending: true }).map((i) => i.tag)).toEqual(['api:1', 'web:1']);
  });
});
