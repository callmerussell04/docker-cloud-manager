import type { Scope } from '@/shared/api/scope';
import { labelFor, type TFunction, type TranslationKey } from '@/shared/i18n';

export interface ProjectData {
  id: string;
  name: string;
  status: string;
  error_message: string;
  last_error: string;
  created_at: number;
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
