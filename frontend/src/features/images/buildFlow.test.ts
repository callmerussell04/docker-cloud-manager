import { describe, expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import { buildStepIssues, isBuildArchive, type BuildImageForm } from './buildFlow';

const t = ((key: string) => key) as TFunction;

function form(overrides: Partial<BuildImageForm> = {}): BuildImageForm {
  return { source: 'archive', repo_url: '', ref: '', tag: 'shop:v1', context: '.', dockerfile: 'Dockerfile', build_args: [], ...overrides };
}

const archive = { name: 'shop.tar.gz' };

test.each([
  ['app.zip', true],
  ['app.TAR', true],
  ['app.tar.gz', true],
  ['app.tgz', true],
  ['compose.yml', false],
  ['notes.txt', false],
])('isBuildArchive(%s) -> %s', (name, expected) => {
  expect(isBuildArchive(name)).toBe(expected);
});

describe('buildStepIssues on Source', () => {
  test('an upload needs an archive', () => {
    expect(buildStepIssues(form(), 'source', t, { file: null, gitEnabled: true })).toEqual([{ field: 'file', message: 'validation.fileRequired' }]);
    expect(buildStepIssues(form(), 'source', t, { file: { name: 'notes.txt' }, gitEnabled: true })).toEqual([{ field: 'file', message: 'validation.buildFileType' }]);
    expect(buildStepIssues(form(), 'source', t, { file: archive, gitEnabled: true })).toEqual([]);
  });

  test('a Git source needs a repository while Git is on', () => {
    expect(buildStepIssues(form({ source: 'git', repo_url: ' ' }), 'source', t, { file: null, gitEnabled: true })).toEqual([
      { field: 'repo_url', message: 'validation.gitUrlRequired' },
    ]);
    expect(buildStepIssues(form({ source: 'git', repo_url: 'https://github.com/acme/shop.git' }), 'source', t, { file: null, gitEnabled: false })).toEqual([
      { field: 'source', message: 'common.gitUnavailable' },
    ]);
    expect(buildStepIssues(form({ source: 'git', repo_url: 'https://github.com/acme/shop.git' }), 'source', t, { file: null, gitEnabled: true })).toEqual([]);
  });
});

describe('buildStepIssues on Image', () => {
  const context = { file: archive, gitEnabled: true };

  test('the image needs a tag', () => {
    expect(buildStepIssues(form({ tag: '  ' }), 'image', t, context)).toEqual([{ field: 'tag', message: 'validation.imageRequired' }]);
  });

  test('every build argument needs a name', () => {
    expect(buildStepIssues(form({ build_args: [{ key: 'VERSION', value: '1' }, { key: '', value: 'x' }] }), 'image', t, context)).toEqual([
      { field: 'build_args.1.key', message: 'validation.keyRequired' },
    ]);
  });

  test('a complete image step has nothing to fix, and Review checks nothing of its own', () => {
    expect(buildStepIssues(form(), 'image', t, context)).toEqual([]);
    expect(buildStepIssues(form({ tag: '' }), 'review', t, context)).toEqual([]);
  });
});
