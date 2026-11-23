import { describe, expect, test } from 'vitest';
import type { ContainerData } from '@/features/containers/model';
import type { TFunction } from '@/shared/i18n';
import {
  blockedProjectActions,
  defaultProjectSort,
  deployProgress,
  matchesProjectSearch,
  projectStatusText,
  projectSummary,
  projectTone,
  serviceSummary,
  shownSource,
  servicesByProject,
  sortProjects,
  type ProjectData,
  type ProjectListItem,
} from './model';

const t = ((key: string, params?: Record<string, unknown>) => (params ? `${key} ${JSON.stringify(params)}` : key)) as TFunction;
const MiB = 1024 * 1024;

function project(overrides: Partial<ProjectData> = {}): ProjectData {
  return { id: overrides.name ?? 'p', name: 'shop', status: 'running', error_message: '', last_error: '', created_at: 1_700_000_000, ...overrides };
}

function service(overrides: Partial<ContainerData> = {}): ContainerData {
  return {
    id: overrides.name ?? 'c',
    name: 'web',
    image_tag: 'nginx:1',
    internal_port: 0,
    domain_prefix: '',
    status: 'running',
    last_error: '',
    created_at: 1_700_000_000,
    memory_reservation_bytes: 256 * MiB,
    cpu_reservation_millicores: 250,
    ...overrides,
  };
}

function item(projectOverrides: Partial<ProjectData>, services: ContainerData[] = []): ProjectListItem {
  return { project: project(projectOverrides), services };
}

describe('projectTone', () => {
  test.each([
    ['running', 'ok'],
    ['stopped', 'idle'],
    ['canceled', 'idle'],
    ['failed', 'bad'],
    ['deploying', 'busy'],
    ['pending', 'busy'],
    ['deleting', 'busy'],
    ['something-new', 'busy'],
  ] as const)('a %s project with healthy services is %s', (status, tone) => {
    expect(projectTone(status, [service()])).toBe(tone);
  });

  test('a running project with a failing service needs attention', () => {
    expect(projectTone('running', [service(), service({ name: 'db', status: 'error' })])).toBe('bad');
  });
});

describe('serviceSummary', () => {
  test('counts the services, those running and those failing, and adds up their reservations', () => {
    const summary = serviceSummary([
      service({ name: 'web', domain_prefix: 'shop' }),
      service({ name: 'db', status: 'exited', memory_reservation_bytes: 512 * MiB }),
      service({ name: 'worker', status: 'missing', domain_prefix: 'jobs' }),
    ]);
    expect(summary).toEqual({ total: 3, running: 1, failing: 1, addresses: ['shop', 'jobs'], memoryBytes: 1024 * MiB });
  });

  test('a project with no services yet has nothing', () => {
    expect(serviceSummary([])).toEqual({ total: 0, running: 0, failing: 0, addresses: [], memoryBytes: 0 });
  });
});

describe('servicesByProject', () => {
  test('groups Compose containers under their project, in name order, and leaves standalone ones out', () => {
    const grouped = servicesByProject([
      service({ name: 'web', project_id: 'a' }),
      service({ name: 'solo' }),
      service({ name: 'db', project_id: 'a' }),
      service({ name: 'api', project_id: 'b' }),
    ]);
    expect([...grouped.keys()].sort()).toEqual(['a', 'b']);
    expect(grouped.get('a')?.map((container) => container.name)).toEqual(['db', 'web']);
  });
});

describe('projectSummary', () => {
  test('a running project says how many services run and how many fail', () => {
    expect(projectSummary(t, project(), [service(), service({ name: 'db', status: 'error' })])).toEqual({
      text: 'projects.servicesRunning {"running":1,"total":2} · projects.servicesFailing {"count":1}',
      bad: true,
    });
  });

  test('a healthy running project is not marked', () => {
    expect(projectSummary(t, project(), [service()])).toEqual({ text: 'projects.servicesRunning {"running":1,"total":1}', bad: false });
  });

  test('a failed project leads with its error', () => {
    expect(projectSummary(t, project({ status: 'failed', error_message: 'build failed at step 4' }), [])).toEqual({ text: 'build failed at step 4', bad: true });
  });

  test('a failed project without a recorded error says it failed', () => {
    expect(projectSummary(t, project({ status: 'failed' }), [])).toEqual({ text: 'status.failed', bad: true });
  });

  test('a deploying project names the step it is on when the API reports it', () => {
    expect(projectSummary(t, project({ status: 'deploying', deploy_stage: 'building' }), []).text).toBe('projects.stage.building');
  });

  test('a deploying project without a reported step shows its status', () => {
    expect(projectSummary(t, project({ status: 'deploying' }), [])).toEqual({ text: 'status.deploying', bad: false });
  });

  test('a stopped project says none of its services run', () => {
    expect(projectSummary(t, project({ status: 'stopped' }), [service({ status: 'exited' })])).toEqual({ text: 'projects.servicesRunning {"running":0,"total":1}', bad: false });
  });

  test('a stopped project with no services shows its status', () => {
    expect(projectSummary(t, project({ status: 'stopped' }), []).text).toBe('status.stopped');
  });
});

describe('projectStatusText', () => {
  test('a running project with failing services says how many', () => {
    expect(projectStatusText(t, 'running', 2)).toBe('status.running · projects.servicesFailing {"count":2}');
  });

  test.each([['running', 0], ['failed', 0], ['stopped', 3]] as const)('a %s project with %i failing is its status', (status, failing) => {
    expect(projectStatusText(t, status, failing)).toBe(`status.${status}`);
  });
});

describe('shownSource', () => {
  test('names a Git repository, and a file when the API gives one', () => {
    expect(shownSource({ kind: 'git', repo_url: 'https://github.com/acme/docs' })).toEqual({ kind: 'git', repo_url: 'https://github.com/acme/docs' });
    expect(shownSource({ kind: 'yaml', file: 'wiki.yml' })).toEqual({ kind: 'yaml', file: 'wiki.yml' });
  });

  test.each([[undefined], [{ kind: 'archive' as const }], [{ kind: 'git' as const, repo_url: ' ' }]])('shows nothing for %j', (source) => {
    expect(shownSource(source)).toBeUndefined();
  });
});

describe('deployProgress', () => {
  test('marks the steps before the current one done and the rest to come', () => {
    expect(deployProgress('creating')).toEqual([
      { stage: 'planning', state: 'done' },
      { stage: 'building', state: 'done' },
      { stage: 'creating', state: 'current' },
      { stage: 'starting', state: 'next' },
    ]);
  });

  test.each([[undefined], [''], ['rolling_back'], ['unknown']])('shows no steps for %j', (stage) => {
    expect(deployProgress(stage)).toBeUndefined();
  });
});

describe('blockedProjectActions', () => {
  test('a running project can stop and be deleted, not start or cancel', () => {
    expect(blockedProjectActions('running')).toEqual({ start: true, stop: false, cancel: true, delete: false });
  });

  test('a stopped project can start', () => {
    expect(blockedProjectActions('stopped')).toEqual({ start: false, stop: true, cancel: true, delete: false });
  });

  test('a deploying project can only be canceled', () => {
    expect(blockedProjectActions('deploying')).toEqual({ start: true, stop: true, cancel: false, delete: true });
  });

  test('a failed or canceled project can only be deleted', () => {
    expect(blockedProjectActions('failed')).toEqual({ start: true, stop: true, cancel: true, delete: false });
    expect(blockedProjectActions('canceled')).toEqual({ start: true, stop: true, cancel: true, delete: false });
  });
});

describe('sortProjects', () => {
  const items = [
    item({ id: 'a', name: 'alpha', created_at: 300 }, [service({ memory_reservation_bytes: 100 })]),
    item({ id: 'b', name: 'beta', status: 'failed', created_at: 100 }),
    item({ id: 'c', name: 'gamma', status: 'stopped', created_at: 200 }, [service({ status: 'exited', domain_prefix: 'g', memory_reservation_bytes: 900 })]),
    item({ id: 'd', name: 'delta', status: 'deploying', created_at: 400 }),
  ];
  const names = (sorted: ProjectListItem[]) => sorted.map((entry) => entry.project.name);

  test('starts failing first, then changing, running and stopped', () => {
    expect(names(sortProjects(items, defaultProjectSort))).toEqual(['beta', 'delta', 'alpha', 'gamma']);
  });

  test('sorts by memory, the largest last', () => {
    expect(names(sortProjects(items, { key: 'memory', ascending: true }))).toEqual(['beta', 'delta', 'alpha', 'gamma']);
  });

  test('sorts by age, the youngest first', () => {
    expect(names(sortProjects(items, { key: 'created', ascending: true }))).toEqual(['delta', 'alpha', 'gamma', 'beta']);
  });

  test('sorts published projects first by address', () => {
    expect(names(sortProjects(items, { key: 'address', ascending: true }))[0]).toBe('gamma');
  });
});

describe('matchesProjectSearch', () => {
  const entry = item({ name: 'Storefront' }, [service({ name: 'payments-api' })]);

  test.each([
    ['', true],
    ['store', true],
    ['PAYMENTS', true],
    ['wiki', false],
  ])('%j matches: %s', (search, expected) => {
    expect(matchesProjectSearch(entry, search)).toBe(expected);
  });
});
