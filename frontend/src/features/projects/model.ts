import { containerTone, type ContainerData } from '@/features/containers/model';
import { getServerErrorMessage } from '@/shared/api/apiError';
import type { Scope } from '@/shared/api/scope';
import { labelFor, type TFunction, type TranslationKey } from '@/shared/i18n';
import { sortRows, type ListSort } from '@/shared/lib/listState';
import type { StatusTone } from '@/shared/ui/StatusDot';

// ProjectSource is where a project was deployed from: an uploaded archive, a
// raw Compose YAML file, or a Git repository at a ref.
export type ProjectSource =
  | { kind: 'archive'; file?: string }
  | { kind: 'yaml'; file?: string }
  | { kind: 'git'; repo_url: string; ref?: string };

// DeployStage is the step a Compose deployment is on, as Core's deployment
// job records it.
export type DeployStage = 'planning' | 'building' | 'creating' | 'starting';

export interface ProjectData {
  id: string;
  name: string;
  status: string;
  error_message: string;
  last_error: string;
  created_at: number;
  // The API does not send the source, the Compose file path or the deploy
  // stage yet (a backend follow-up of #171); each is shown only when present.
  source?: ProjectSource;
  compose_file?: string;
  deploy_stage?: string;
}

export interface AdminProjectData extends ProjectData {
  owner_id?: string;
  owner_username?: string;
}

// ProjectOf is the project a list returns in a scope; the all-users view adds
// the owner.
export type ProjectOf<S extends Scope> = S extends 'all' ? AdminProjectData : ProjectData;

export type ProjectAction = 'start' | 'stop' | 'cancel' | 'delete';

export interface CreateProjectGitPayload {
  project_name: string;
  repo_url: string;
  ref?: string;
  compose_file?: string;
}

const busyProjectStatuses = new Set(['pending', 'building', 'deploying', 'canceling', 'starting', 'stopping', 'deleting']);

// isProjectBusy reports whether a project is in a transitional state: its
// actions are blocked and its list keeps polling.
export function isProjectBusy(status: string) {
  return busyProjectStatuses.has(status);
}

// projectTone is what a project's status means to the user: running is ok
// unless a service is failing, failed is failing, stopped and canceled are at
// rest, and every other status, including one this client does not know, is
// changing.
export function projectTone(status: string, services: readonly ContainerData[]): StatusTone {
  if (status === 'failed') return 'bad';
  if (status === 'running') return services.some((service) => containerTone(service.status) === 'bad') ? 'bad' : 'ok';
  if (status === 'stopped' || status === 'canceled') return 'idle';
  return 'busy';
}

export interface ServiceSummary {
  total: number;
  running: number;
  failing: number;
  // addresses are the published services' subdomain prefixes, in order.
  addresses: string[];
  // memoryBytes is what the services reserve whenever they run.
  memoryBytes: number;
}

// serviceSummary counts a project's services (its containers), those running
// and those failing, lists their published subdomains and adds up their
// memory reservations.
export function serviceSummary(services: readonly ContainerData[]): ServiceSummary {
  return {
    total: services.length,
    running: services.filter((service) => service.status === 'running').length,
    failing: services.filter((service) => containerTone(service.status) === 'bad').length,
    addresses: services.filter((service) => service.domain_prefix).map((service) => service.domain_prefix),
    memoryBytes: services.reduce((sum, service) => sum + service.memory_reservation_bytes, 0),
  };
}

// servicesByProject groups Compose containers by their project, each group in
// name order; standalone containers are left out.
export function servicesByProject<C extends ContainerData>(containers: readonly C[]): Map<string, C[]> {
  const grouped = new Map<string, C[]>();
  for (const container of sortRows(containers, (entry) => [entry.name, entry.id], true)) {
    if (!container.project_id) continue;
    grouped.set(container.project_id, [...(grouped.get(container.project_id) ?? []), container]);
  }
  return grouped;
}

export const deployStages: readonly DeployStage[] = ['planning', 'building', 'creating', 'starting'];

export interface DeployProgressStep {
  stage: DeployStage;
  state: 'done' | 'current' | 'next';
}

// deployProgress lays out a deployment's steps with the one it is on marked;
// nothing when the API reports no stage, or one that is not a step forward
// (a rollback).
export function deployProgress(stage: string | undefined): DeployProgressStep[] | undefined {
  const current = deployStages.indexOf(stage as DeployStage);
  if (current < 0) return undefined;
  return deployStages.map((entry, index) => ({
    stage: entry,
    state: index < current ? 'done' : index === current ? 'current' : 'next',
  }));
}

// shownSource is a project's source when there is something to name: a Git
// repository, or a file the API names; otherwise nothing.
export function shownSource(source: ProjectSource | undefined): ProjectSource | undefined {
  if (!source) return undefined;
  if (source.kind === 'git') return source.repo_url.trim() ? source : undefined;
  return source.file ? source : undefined;
}

// projectStatusText is a project's status for its page's title: a running
// project with failing services says how many beside its red mark.
export function projectStatusText(t: TFunction, status: string, failing: number) {
  const label = projectStatusLabel(t, status);
  return status === 'running' && failing > 0 ? `${label} · ${t('projects.servicesFailing', { count: failing })}` : label;
}

// projectFailure is a failed project's error, mapped and safe to show.
export function projectFailure(t: TFunction, project: Pick<ProjectData, 'error_message' | 'last_error'>) {
  return getServerErrorMessage(project.error_message || project.last_error, t);
}

// projectSummary is a project's state in one line: a failed project's error,
// how many of its services run (and fail) while it runs or is stopped, the
// step a deployment is on, or else its status. bad marks a line about a failure.
export function projectSummary(t: TFunction, project: ProjectData, services: readonly ContainerData[]): { text: string; bad: boolean } {
  const summary = serviceSummary(services);
  if (project.status === 'failed') return { text: projectFailure(t, project) ?? projectStatusLabel(t, project.status), bad: true };
  if ((project.status === 'running' || project.status === 'stopped') && summary.total > 0) {
    const running = t('projects.servicesRunning', { running: summary.running, total: summary.total });
    return summary.failing > 0
      ? { text: `${running} · ${t('projects.servicesFailing', { count: summary.failing })}`, bad: true }
      : { text: running, bad: false };
  }
  const step = deployProgress(project.deploy_stage)?.find((entry) => entry.state === 'current');
  if (step && isProjectBusy(project.status)) return { text: t(`projects.stage.${step.stage}`), bad: false };
  return { text: projectStatusLabel(t, project.status), bad: false };
}

export interface BlockedProjectActions {
  start: boolean;
  stop: boolean;
  cancel: boolean;
  delete: boolean;
}

// blockedProjectActions is which actions a project's status refuses: start
// and stop only from rest (a failed or canceled deployment has nothing to
// run), cancel only while a deployment builds or deploys, and delete only
// while no worker is changing the project.
export function blockedProjectActions(status: string): BlockedProjectActions {
  const busy = isProjectBusy(status);
  const settled = status === 'failed' || status === 'canceled';
  return {
    start: busy || settled || status === 'running',
    stop: busy || settled || status === 'stopped',
    cancel: !canCancelProject(status),
    delete: busy,
  };
}

// ProjectListItem is a project with its services, as the list shows it.
export interface ProjectListItem {
  project: ProjectData;
  services: ContainerData[];
}

// ProjectSortKey is a projects list column.
export type ProjectSortKey = 'name' | 'services' | 'address' | 'memory' | 'created';

export const projectSortKeys: readonly ProjectSortKey[] = ['name', 'services', 'address', 'memory', 'created'];

// The list starts with failing projects first.
export const defaultProjectSort: ListSort<ProjectSortKey> = { key: 'services', ascending: true };

const toneRank: Record<StatusTone, number> = { bad: 0, busy: 1, ok: 2, idle: 3 };

function projectSortValue({ project, services }: ProjectListItem, key: ProjectSortKey): string | number {
  switch (key) {
    case 'name':
      return project.name;
    case 'services':
      return toneRank[projectTone(project.status, services)];
    case 'address': {
      // Published projects first, by their first address; the rest after.
      const first = serviceSummary(services).addresses[0];
      return first ? `0${first}` : '1';
    }
    case 'memory':
      return serviceSummary(services).memoryBytes;
    case 'created':
      // The column reads as an age, so ascending is the youngest first.
      return -project.created_at;
  }
}

// sortProjects orders a projects list by one column. Ascending Services is
// failing first, then changing, running and at rest. Projects that tie keep
// name order, whichever way the column sorts.
export function sortProjects<I extends ProjectListItem>(items: readonly I[], sort: ListSort<ProjectSortKey>): I[] {
  return sortRows(items, (entry) => [projectSortValue(entry, sort.key), entry.project.name, entry.project.id], sort.ascending);
}

// matchesProjectSearch reports whether a project's name, or the name of one
// of its services, contains the search, ignoring case.
export function matchesProjectSearch({ project, services }: ProjectListItem, search: string) {
  const query = search.trim().toLowerCase();
  return !query || project.name.toLowerCase().includes(query) || services.some((service) => service.name.toLowerCase().includes(query));
}

// canCancelProject reports whether a project's deployment can be canceled.
export function canCancelProject(status: string) {
  return status === 'building' || status === 'deploying';
}

const statusKeys: Record<string, TranslationKey> = {
  pending: 'status.pending',
  building: 'status.building',
  deploying: 'status.deploying',
  running: 'status.running',
  starting: 'status.starting',
  stopping: 'status.stopping',
  stopped: 'status.stopped',
  canceling: 'status.canceling',
  canceled: 'status.canceled',
  deleting: 'status.deleting',
  failed: 'status.failed',
};

export function projectStatusLabel(t: TFunction, status?: string) {
  return labelFor(t, statusKeys, status);
}
