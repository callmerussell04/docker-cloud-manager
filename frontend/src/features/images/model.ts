import type { Scope } from '@/shared/api/scope';
import { labelFor, type TFunction, type TranslationKey } from '@/shared/i18n';

export interface ImageData {
  id: string;
  tag: string;
  size_bytes: number;
  status: string;
  last_error: string;
  created_at: number;
}

export interface AdminImageData extends ImageData {
  owner_id?: string;
  owner_username?: string;
}

export interface BuildData {
  id: string;
  // Absent once the build's image record has been deleted.
  image_id?: string;
  // The tag the build was queued for.
  tag?: string;
  project_id?: string;
  project_service_name?: string;
  status: string;
  created_at: number;
  started_at: number;
  finished_at: number;
}

export interface AdminBuildData extends BuildData {
  log_object_key: string;
  owner_id?: string;
  owner_username?: string;
}

// ImageOf and BuildOf are what a list returns in a scope; the all-users view
// adds the owner.
export type ImageOf<S extends Scope> = S extends 'all' ? AdminImageData : ImageData;
export type BuildOf<S extends Scope> = S extends 'all' ? AdminBuildData : BuildData;

export interface BuildAvailability {
  enabled: boolean;
  message: string;
  git_sources_enabled?: boolean;
  git_message?: string;
}

export interface CreateBuildGitPayload {
  repo_url: string;
  ref?: string;
  tag: string;
  context?: string;
  dockerfile?: string;
  build_args?: Record<string, string>;
}

const activeBuildStatuses = new Set(['pending', 'running']);
const finishedBuildStatuses = new Set(['success', 'failed', 'failed_timeout', 'failed_quota_exceeded', 'failed_resource_exhausted', 'failed_internal', 'canceled']);

// isBuildActive reports whether a build is queued or running: it can be
// canceled but not deleted, and its list keeps polling.
export function isBuildActive(status: string) {
  return activeBuildStatuses.has(status);
}

// buildLogsAvailable reports whether a build has finished and stored its log.
export function buildLogsAvailable(status: string) {
  return finishedBuildStatuses.has(status);
}

const imageStatusKeys: Record<string, TranslationKey> = {
  available: 'status.available',
  building: 'status.building',
  deleting: 'status.deleting',
  error: 'status.error',
  missing: 'status.missing',
};

const buildStatusKeys: Record<string, TranslationKey> = {
  pending: 'status.pending',
  running: 'status.running',
  success: 'status.success',
  failed: 'status.failed',
  failed_timeout: 'status.failedTimeout',
  failed_quota_exceeded: 'status.failedQuotaExceeded',
  failed_resource_exhausted: 'status.failedResourceExhausted',
  failed_internal: 'status.failedInternal',
  canceled: 'status.canceled',
};

export function imageStatusLabel(t: TFunction, status?: string) {
  return labelFor(t, imageStatusKeys, status);
}

export function buildStatusLabel(t: TFunction, status?: string) {
  return labelFor(t, buildStatusKeys, status);
}
