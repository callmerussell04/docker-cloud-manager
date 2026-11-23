import { describe, expect, test } from 'vitest';
import type { TFunction } from '@/shared/i18n';
import { deployStepIssues, isRawComposeFile, projectNameFrom, type DeployProjectForm } from './createFlow';

const t = ((key: string) => key) as TFunction;

function form(overrides: Partial<DeployProjectForm> = {}): DeployProjectForm {
  return { source: 'upload', project_name: 'shop', repo_url: '', ref: '', compose_file: '', ...overrides };
}

const archive = { name: 'shop-1.2.tar.gz' };

describe('deployStepIssues', () => {
  test('an upload needs a file', () => {
    expect(deployStepIssues(form(), 'source', t, { file: null, gitEnabled: true })).toEqual([
      { field: 'file', message: 'validation.fileRequired' },
    ]);
  });

  test('an upload takes only an archive or a YAML file', () => {
    expect(deployStepIssues(form(), 'source', t, { file: { name: 'notes.txt' }, gitEnabled: true })).toEqual([
      { field: 'file', message: 'validation.projectFileType' },
    ]);
    expect(deployStepIssues(form(), 'source', t, { file: { name: 'compose.YAML' }, gitEnabled: true })).toEqual([]);
    expect(deployStepIssues(form(), 'source', t, { file: archive, gitEnabled: true })).toEqual([]);
  });

  test('a Git source needs a repository URL', () => {
    expect(deployStepIssues(form({ source: 'git', repo_url: '  ' }), 'source', t, { file: null, gitEnabled: true })).toEqual([
      { field: 'repo_url', message: 'validation.gitUrlRequired' },
    ]);
    expect(deployStepIssues(form({ source: 'git', repo_url: 'https://github.com/acme/shop.git' }), 'source', t, { file: null, gitEnabled: true })).toEqual([]);
  });

  test('a Git source is refused while Git sources are turned off', () => {
    expect(deployStepIssues(form({ source: 'git', repo_url: 'https://github.com/acme/shop.git' }), 'source', t, { file: null, gitEnabled: false })).toEqual([
      { field: 'source', message: 'common.gitUnavailable' },
    ]);
  });

  test('the project step checks only the name', () => {
    expect(deployStepIssues(form({ project_name: '' }), 'project', t, { file: null, gitEnabled: true })).toEqual([
      { field: 'project_name', message: 'validation.projectNameRequired' },
    ]);
    expect(deployStepIssues(form({ project_name: '-shop!' }), 'project', t, { file: null, gitEnabled: true })).toEqual([
      { field: 'project_name', message: 'validation.projectNameFormat' },
    ]);
    expect(deployStepIssues(form({ project_name: 'shop_2.0-beta' }), 'project', t, { file: null, gitEnabled: true })).toEqual([]);
  });

  test('the review step has nothing of its own to check', () => {
    expect(deployStepIssues(form({ project_name: '' }), 'review', t, { file: null, gitEnabled: true })).toEqual([]);
  });
});

describe('projectNameFrom', () => {
  test.each([
    [{ source: 'upload', fileName: 'Shop Front-1.2.tar.gz' }, 'shop-front-1.2'],
    [{ source: 'upload', fileName: 'docker-compose.yml' }, 'docker-compose'],
    [{ source: 'upload', fileName: 'site.tgz' }, 'site'],
    [{ source: 'git', repoUrl: 'https://github.com/acme/Observability.git' }, 'observability'],
    [{ source: 'git', repoUrl: 'https://github.com/acme/docs/' }, 'docs'],
    [{ source: 'git', repoUrl: '' }, ''],
    [{ source: 'upload' }, ''],
    [{ source: 'upload', fileName: '__.zip' }, ''],
  ] as const)('names %j %j', (input, expected) => {
    expect(projectNameFrom(input)).toBe(expected);
  });

  test('keeps the name within what Core accepts', () => {
    expect(projectNameFrom({ source: 'upload', fileName: `${'a'.repeat(80)}.zip` })).toHaveLength(63);
  });
});

describe('isRawComposeFile', () => {
  test.each([
    ['compose.yml', true],
    ['stack.YAML', true],
    ['app.tar.gz', false],
  ])('%s: %s', (name, expected) => {
    expect(isRawComposeFile(name)).toBe(expected);
  });
});
