import { describe, expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import {
  activeBuildCount,
  buildAgainFrom,
  buildPurpose,
  buildTiming,
  buildTone,
  defaultBuildSort,
  formatDuration,
  isBuildErrorLine,
  matchesBuildSearch,
  sortBuilds,
  type BuildData,
  type ImageData,
} from './model';

const t = ((key: string, params?: Record<string, string>) => (params ? `${key} ${JSON.stringify(params)}` : key)) as TFunction;

function build(overrides: Partial<BuildData> = {}): BuildData {
  return {
    id: overrides.tag ?? 'id',
    tag: 'shop:v1',
    status: 'success',
    created_at: 1_000,
    started_at: 1_010,
    finished_at: 1_100,
    ...overrides,
  };
}

test.each([
  ['success', 'ok'],
  ['pending', 'busy'],
  ['running', 'busy'],
  ['canceled', 'idle'],
  ['failed', 'bad'],
  ['failed_timeout', 'bad'],
  ['failed_internal', 'bad'],
])('buildTone(%s) -> %s', (status, tone) => {
  expect(buildTone(status)).toBe(tone);
});

describe('buildPurpose', () => {
  test('a Compose build was for its project service', () => {
    expect(buildPurpose(build({ project_id: 'p', project_service_name: 'web', source_kind: 'git' }))).toEqual({ kind: 'service', service: 'web' });
  });

  test('a Git build names its repository without the scheme, and its ref', () => {
    expect(buildPurpose(build({ source_kind: 'git', source_location: 'https://github.com/acme/app.git', source_ref: 'main' }))).toEqual({
      kind: 'git', repository: 'github.com/acme/app.git', ref: 'main',
    });
  });

  test('an upload names its file; a build with no recorded source says so', () => {
    expect(buildPurpose(build({ source_kind: 'archive', source_location: 'app.zip' }))).toEqual({ kind: 'archive', file: 'app.zip' });
    expect(buildPurpose(build())).toEqual({ kind: 'unknown' });
  });
});

describe('buildTiming', () => {
  const now = 2_000;

  test('a queued build has waited since it was queued', () => {
    expect(buildTiming(build({ status: 'pending', started_at: 0, finished_at: 0 }), now)).toEqual({ phase: 'queued', seconds: 1_000 });
  });

  test('a running build has run since it started', () => {
    expect(buildTiming(build({ status: 'running', started_at: 1_900, finished_at: 0 }), now)).toEqual({ phase: 'running', seconds: 100 });
  });

  test('a finished build took from start to finish', () => {
    expect(buildTiming(build(), now)).toEqual({ phase: 'finished', seconds: 90 });
  });

  test('a build canceled before it started took no time to report', () => {
    expect(buildTiming(build({ status: 'canceled', started_at: 0, finished_at: 1_050 }), now)).toEqual({ phase: 'none' });
  });
});

test('formatDuration reads in seconds, then minutes, then hours', () => {
  expect(formatDuration(t, 42)).toBe('images.duration.seconds {"seconds":42}');
  expect(formatDuration(t, 125)).toBe('images.duration.minutes {"minutes":2,"seconds":"05"}');
  expect(formatDuration(t, 3_720)).toBe('images.duration.hours {"hours":1,"minutes":"02"}');
});

describe('sortBuilds', () => {
  const failed = build({ id: 'f', tag: 'collector:0.7', status: 'failed', created_at: 10 });
  const running = build({ id: 'r', tag: 'landing:2', status: 'running', created_at: 30 });
  const older = build({ id: 'o', tag: 'web:1', created_at: 20, started_at: 20, finished_at: 200 });
  const newer = build({ id: 'n', tag: 'api:1', created_at: 40, started_at: 40, finished_at: 50 });

  test('starts with failing builds, then active ones, then the rest newest first', () => {
    expect(sortBuilds([older, newer, running, failed], defaultBuildSort, 1_000).map((b) => b.id)).toEqual(['f', 'r', 'n', 'o']);
  });

  test('sorts by how long it took and when it was queued', () => {
    expect(sortBuilds([newer, older], { key: 'duration', ascending: false }, 1_000).map((b) => b.id)).toEqual(['o', 'n']);
    expect(sortBuilds([older, newer], { key: 'queued', ascending: true }, 1_000).map((b) => b.id)).toEqual(['n', 'o']);
  });
});

test('matchesBuildSearch finds the tag, the service or the source', () => {
  expect(matchesBuildSearch(build({ tag: 'Shop:v1' }), 'shop')).toBe(true);
  expect(matchesBuildSearch(build({ project_service_name: 'web' }), 'WEB')).toBe(true);
  expect(matchesBuildSearch(build({ source_kind: 'git', source_location: 'https://github.com/acme/app.git' }), 'acme')).toBe(true);
  expect(matchesBuildSearch(build(), 'nothing')).toBe(false);
});

test('activeBuildCount counts queued and running builds', () => {
  expect(activeBuildCount([build(), build({ status: 'pending' }), build({ status: 'running' }), build({ status: 'failed' })])).toBe(2);
});

test.each([
  ['error building image: failed to get filesystem', true],
  ['ERROR: process "/bin/sh -c npm ci" did not complete', true],
  ['Build failed because it exceeded the maximum build time.', true],
  ['fatal: repository not found', true],
  ['INFO[0003] Running: [/bin/sh -c npm run build]', false],
  ['added 842 packages, 0 errors-free', false],
])('isBuildErrorLine(%s) -> %s', (line, expected) => {
  expect(isBuildErrorLine(line)).toBe(expected);
});

describe('buildAgainFrom', () => {
  const shop: ImageData = { id: 'img', tag: 'shop:v1', size_bytes: 0, status: 'available', last_error: '', created_at: 0, used_by: [] };

  test('takes the tag and the latest build source of the image', () => {
    const builds = [
      build({ id: 'old', image_id: 'img', created_at: 10, source_kind: 'archive', source_location: 'shop.zip' }),
      build({ id: 'new', tag: 'shop:v1', created_at: 20, source_kind: 'git', source_location: 'https://github.com/acme/shop.git', source_ref: 'v1' }),
      build({ id: 'other', tag: 'web:v1', created_at: 30, source_kind: 'git', source_location: 'https://github.com/acme/web.git' }),
    ];
    expect(buildAgainFrom(shop, builds)).toEqual({ tag: 'shop:v1', source: 'git', repoUrl: 'https://github.com/acme/shop.git', ref: 'v1' });
  });

  test('an upload or an unknown source starts on an upload with the tag', () => {
    expect(buildAgainFrom(shop, [build({ image_id: 'img', source_kind: 'archive', source_location: 'shop.zip' })])).toEqual({ tag: 'shop:v1', source: 'archive' });
    expect(buildAgainFrom(shop, [])).toEqual({ tag: 'shop:v1', source: 'archive' });
  });
});
