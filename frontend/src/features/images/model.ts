import type { Scope } from '@/shared/api/scope';
import { labelFor, type TFunction, type TranslationKey } from '@/shared/i18n';
import { sortRows, type ListSort } from '@/shared/lib/listState';
import { matchesSearch, shortNames } from '@/shared/lib/names';
import { statusToneRank, type StatusTone } from '@/shared/ui/statusTone';

// ImageUser is a container created from an image.
export interface ImageUser {
  id: string;
  name: string;
}

export interface ImageData {
  id: string;
  tag: string;
  size_bytes: number;
  status: string;
  last_error: string;
  created_at: number;
  // The containers created from the image, by name. Core refuses to delete
  // an image while any container uses it.
  used_by: ImageUser[];
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
  // Where the build's context came from: an uploaded archive (its file name)
  // or a Git repository (its URL and the ref asked for). Absent for a
  // Compose project's build.
  source_kind?: 'archive' | 'git';
  source_location?: string;
  source_ref?: string;
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

// isImageAvailable reports whether the image is ready to run a container
// from.
export function isImageAvailable(status: string) {
  return status === 'available';
}

// isImageBusy reports whether the image is still being built or deleted; its
// list keeps polling while it is.
export function isImageBusy(status: string) {
  return status === 'building' || status === 'deleting';
}

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

// imageTone is what an image's status means to the user: available is ok,
// error and missing are failing, and building, deleting or a status this
// client does not know is changing.
export function imageTone(status: string): StatusTone {
  if (status === 'available') return 'ok';
  if (status === 'error' || status === 'missing') return 'bad';
  return 'busy';
}

// ImageDeleteBlock is why an image cannot be deleted now: a container uses
// it, it failed, or it is still building or being deleted. Core refuses
// every status but available and missing as a conflict.
export type ImageDeleteBlock = 'inUse' | 'failed' | 'busy';

export function imageDeleteBlock(image: Pick<ImageData, 'status' | 'used_by'>): ImageDeleteBlock | undefined {
  if (image.used_by.length > 0) return 'inUse';
  if (image.status === 'available' || image.status === 'missing') return undefined;
  return image.status === 'error' ? 'failed' : 'busy';
}

// imageUsersLabel names the containers using an image: up to two by name,
// then how many more; nothing when none does.
export function imageUsersLabel(t: TFunction, users: readonly ImageUser[]) {
  return shortNames(users, (names, count) => t('images.usedByMore', { names, count }));
}

// imageNameLines splits an image tag into the part a user recognises, its
// repository and tag, and the registry path before it, shown beneath; a tag
// with no path is its own name.
export function imageNameLines(tag: string): { name: string; path?: string } {
  const slash = tag.lastIndexOf('/');
  if (slash < 0) return { name: tag, path: undefined };
  return { name: tag.slice(slash + 1), path: tag.slice(0, slash) };
}

// matchesImageSearch reports whether an image's tag, or the name of a
// container using it, contains the search, ignoring case.
export function matchesImageSearch(image: Pick<ImageData, 'tag' | 'used_by'>, search: string) {
  return matchesSearch(search, [image.tag, ...image.used_by.map((user) => user.name)]);
}

// ImageSortKey is an images list column.
export type ImageSortKey = 'name' | 'status' | 'usedBy' | 'size' | 'created';

export const imageSortKeys: readonly ImageSortKey[] = ['name', 'status', 'usedBy', 'size', 'created'];

// The list starts with failing images first.
export const defaultImageSort: ListSort<ImageSortKey> = { key: 'status', ascending: true };

function imageSortValue(image: ImageData, key: ImageSortKey): string | number {
  switch (key) {
    case 'name':
      return imageNameLines(image.tag).name;
    case 'status':
      return statusToneRank[imageTone(image.status)];
    case 'usedBy':
      return image.used_by.length;
    case 'size':
      return image.size_bytes;
    case 'created':
      // The column reads as an age, so ascending is the youngest first.
      return -image.created_at;
  }
}

// sortImages orders an images list by one column. Ascending status is
// failing first, then changing, then available. Images that tie keep tag
// order, whichever way the column sorts.
export function sortImages<I extends ImageData>(images: readonly I[], sort: ListSort<ImageSortKey>): I[] {
  return sortRows(images, (image) => [imageSortValue(image, sort.key), image.tag, image.id], sort.ascending);
}

// buildTone is what a build's status means: success is ok, queued and
// running are changing, canceled is at rest, and every failure is failing.
export function buildTone(status: string): StatusTone {
  if (status === 'success') return 'ok';
  if (status === 'canceled') return 'idle';
  if (status === 'failed' || status.startsWith('failed_')) return 'bad';
  return 'busy';
}

// activeBuildCount is how many builds are queued or running.
export function activeBuildCount(builds: readonly Pick<BuildData, 'status'>[]) {
  return builds.filter((build) => isBuildActive(build.status)).length;
}

// BuildPurpose is what a build was for: a Compose project's service, a Git
// repository, an uploaded archive, or a source the API did not record.
export type BuildPurpose =
  | { kind: 'service'; service: string }
  | { kind: 'git'; repository: string; ref?: string }
  | { kind: 'archive'; file: string }
  | { kind: 'unknown' };

export function buildPurpose(build: BuildData): BuildPurpose {
  if (build.project_service_name) return { kind: 'service', service: build.project_service_name };
  if (build.source_kind === 'git' && build.source_location) {
    const repository = build.source_location.replace(/^https?:\/\//, '');
    return build.source_ref ? { kind: 'git', repository, ref: build.source_ref } : { kind: 'git', repository };
  }
  if (build.source_kind === 'archive' && build.source_location) return { kind: 'archive', file: build.source_location };
  return { kind: 'unknown' };
}

// BuildTiming is the time a build's list shows: how long it has waited in
// the queue, how long it has run, or how long it took; a build stopped
// before it started has none.
export type BuildTiming = { phase: 'queued' | 'running' | 'finished'; seconds: number } | { phase: 'none' };

export function buildTiming(build: Pick<BuildData, 'status' | 'created_at' | 'started_at' | 'finished_at'>, now: number): BuildTiming {
  if (build.status === 'pending') return { phase: 'queued', seconds: Math.max(0, now - build.created_at) };
  if (build.status === 'running') return { phase: 'running', seconds: Math.max(0, now - (build.started_at || build.created_at)) };
  if (build.started_at > 0 && build.finished_at > 0) return { phase: 'finished', seconds: Math.max(0, build.finished_at - build.started_at) };
  return { phase: 'none' };
}

const twoDigits = (value: number) => String(value).padStart(2, '0');

// formatDuration reads a number of seconds as seconds, minutes and seconds,
// or hours and minutes.
export function formatDuration(t: TFunction, seconds: number) {
  const whole = Math.floor(seconds);
  if (whole < 60) return t('images.duration.seconds', { seconds: whole });
  if (whole < 3600) return t('images.duration.minutes', { minutes: Math.floor(whole / 60), seconds: twoDigits(whole % 60) });
  return t('images.duration.hours', { hours: Math.floor(whole / 3600), minutes: twoDigits(Math.floor((whole % 3600) / 60)) });
}

// matchesBuildSearch reports whether a build's tag, its service or its
// source contains the search, ignoring case.
export function matchesBuildSearch(build: BuildData, search: string) {
  return matchesSearch(search, [build.tag, build.project_service_name, build.source_location, build.source_ref]);
}

// BuildSortKey is a builds list column.
export type BuildSortKey = 'tag' | 'purpose' | 'status' | 'duration' | 'queued';

export const buildSortKeys: readonly BuildSortKey[] = ['tag', 'purpose', 'status', 'duration', 'queued'];

// The list starts with failing builds first.
export const defaultBuildSort: ListSort<BuildSortKey> = { key: 'status', ascending: true };

function purposeText(purpose: BuildPurpose) {
  switch (purpose.kind) {
    case 'service': return purpose.service;
    case 'git': return purpose.repository;
    case 'archive': return purpose.file;
    case 'unknown': return '';
  }
}

function buildSortValue(build: BuildData, key: BuildSortKey, now: number): string | number {
  switch (key) {
    case 'tag':
      return build.tag || build.id;
    case 'purpose':
      return purposeText(buildPurpose(build));
    case 'status':
      return statusToneRank[buildTone(build.status)];
    case 'duration': {
      const timing = buildTiming(build, now);
      return timing.phase === 'none' ? -1 : timing.seconds;
    }
    case 'queued':
      // The column reads as an age, so ascending is the youngest first.
      return -build.created_at;
  }
}

// sortBuilds orders a builds list by one column. Ascending status is failing
// first, then queued and running, then succeeded, then canceled. Builds that
// tie keep newest-first order, whichever way the column sorts.
export function sortBuilds<B extends BuildData>(builds: readonly B[], sort: ListSort<BuildSortKey>, now: number): B[] {
  return sortRows(builds, (build) => [buildSortValue(build, sort.key, now), -build.created_at, build.id], sort.ascending);
}

const errorLinePattern = /\b(error|fatal|failed)\b/i;

// isBuildErrorLine reports whether a build log line reports an error, so the
// log shows it in red.
export function isBuildErrorLine(line: string) {
  return errorLinePattern.test(line);
}

// BuildAgain is what Build again fills the build dialog with: the image's
// tag and, when its latest build came from Git, that repository and ref. An
// archive has to be chosen again.
export type BuildAgain = { tag: string; source: 'archive' } | { tag: string; source: 'git'; repoUrl: string; ref: string };

export function buildAgainFrom(image: Pick<ImageData, 'id' | 'tag'>, builds: readonly BuildData[]): BuildAgain {
  const latest = builds
    .filter((build) => build.image_id === image.id || build.tag === image.tag)
    .reduce<BuildData | undefined>((found, build) => (!found || build.created_at > found.created_at ? build : found), undefined);
  if (latest?.source_kind === 'git' && latest.source_location) {
    return { tag: image.tag, source: 'git', repoUrl: latest.source_location, ref: latest.source_ref ?? '' };
  }
  return { tag: image.tag, source: 'archive' };
}
