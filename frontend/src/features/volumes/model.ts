import type { Scope } from '@/shared/api/scope';
import { labelFor, type TFunction, type TranslationKey } from '@/shared/i18n';
import { sortRows, type ListSort } from '@/shared/lib/listState';
import { joinNames, matchesSearch, shortNames } from '@/shared/lib/names';
import { formatBytes } from '@/shared/lib/utils';
import { statusToneRank, type StatusTone } from '@/shared/ui/statusTone';

// VolumeUser is a container that mounts a volume.
export interface VolumeUser {
  id: string;
  name: string;
}

export interface VolumeData {
  id: string;
  name: string;
  status: string;
  last_error: string;
  used_bytes: number;
  usage_observed_at: number;
  size_bytes: number;
  size_enforced: boolean;
  created_at: number;
  // The containers that mount the volume, by name. Core refuses to delete a
  // volume while any container mounts it.
  used_by: VolumeUser[];
}

export interface AdminVolumeData extends VolumeData {
  docker_name: string;
  owner_id?: string;
  owner_username?: string;
}

// VolumeOf is the volume a list returns in a scope; the all-users view adds
// the owner and the Docker volume name.
export type VolumeOf<S extends Scope> = S extends 'all' ? AdminVolumeData : VolumeData;

export const MIB = 1024 * 1024;

export interface CreateVolumeDTO {
  name: string;
  size_bytes?: number;
}

// isVolumeMountable reports whether a new container may mount the volume: any
// volume not being deleted.
export function isVolumeMountable(status: string) {
  return status !== 'deleting';
}

// isVolumeBusy reports whether a background worker is still creating or
// deleting the volume; its list keeps polling while it is.
export function isVolumeBusy(status: string) {
  return status === 'creating' || status === 'deleting';
}

const statusKeys: Record<string, TranslationKey> = {
  available: 'status.available',
  creating: 'status.creating',
  deleting: 'status.deleting',
  error: 'status.error',
  missing: 'status.missing',
};

export function volumeStatusLabel(t: TFunction, status?: string) {
  return labelFor(t, statusKeys, status);
}

// volumeTone is what a volume's status means to the user: available is ok,
// error and missing are failing, and creating, deleting or a status this
// client does not know is changing.
export function volumeTone(status: string): StatusTone {
  if (status === 'available') return 'ok';
  if (status === 'error' || status === 'missing') return 'bad';
  return 'busy';
}

// volumeUsersLabel names the containers using a volume: up to two by name,
// then how many more; nothing when none does.
export function volumeUsersLabel(t: TFunction, users: readonly VolumeUser[]) {
  return shortNames(users, (names, count) => t('volumes.usedByMore', { names, count }));
}

// VolumeDeleteBlock is why a volume cannot be deleted now: a container mounts
// it (Core refuses), or a worker is still creating or deleting it.
export type VolumeDeleteBlock = 'inUse' | 'busy';

export function volumeDeleteBlock(volume: Pick<VolumeData, 'status' | 'used_by'>): VolumeDeleteBlock | undefined {
  if (volume.used_by.length > 0) return 'inUse';
  if (isVolumeBusy(volume.status)) return 'busy';
  return undefined;
}

// volumeDeleteConsequence names what deleting the volume loses: its data,
// with its measured size, or up to its reserved size until usage has been
// measured, and the containers using it.
export function volumeDeleteConsequence(t: TFunction, volume: Pick<VolumeData, 'used_bytes' | 'usage_observed_at' | 'size_bytes' | 'used_by'>) {
  const data = volume.usage_observed_at > 0
    ? t('volumes.deleteConsequence', { size: formatBytes(volume.used_bytes) })
    : t('volumes.deleteConsequenceUnmeasured', { size: formatBytes(volume.size_bytes) });
  const users = volume.used_by.length > 0
    ? t('volumes.deleteUsedBy', { names: joinNames(volume.used_by) })
    : t('volumes.deleteUnused');
  return `${data} ${users}`;
}

// matchesVolumeSearch reports whether a volume's name, or the name of a
// container using it, contains the search, ignoring case.
export function matchesVolumeSearch(volume: Pick<VolumeData, 'name' | 'used_by'>, search: string) {
  return matchesSearch(search, [volume.name, ...volume.used_by.map((user) => user.name)]);
}

// VolumeSortKey is a volumes list column.
export type VolumeSortKey = 'name' | 'status' | 'size' | 'usedBy' | 'created';

export const volumeSortKeys: readonly VolumeSortKey[] = ['name', 'status', 'size', 'usedBy', 'created'];

// The list starts with failing volumes first.
export const defaultVolumeSort: ListSort<VolumeSortKey> = { key: 'status', ascending: true };

function volumeSortValue(volume: VolumeData, key: VolumeSortKey): string | number {
  switch (key) {
    case 'name':
      return volume.name;
    case 'status':
      return statusToneRank[volumeTone(volume.status)];
    case 'size':
      // The space the volume takes; unmeasured reads as nothing yet.
      return volume.usage_observed_at > 0 ? volume.used_bytes : 0;
    case 'usedBy':
      return volume.used_by.length;
    case 'created':
      // The column reads as an age, so ascending is the youngest first.
      return -volume.created_at;
  }
}

// sortVolumes orders a volumes list by one column. Ascending status is
// failing first, then changing, then available. Volumes that tie keep name
// order, whichever way the column sorts.
export function sortVolumes<V extends VolumeData>(volumes: readonly V[], sort: ListSort<VolumeSortKey>): V[] {
  return sortRows(volumes, (volume) => [volumeSortValue(volume, sort.key), volume.name, volume.id], sort.ascending);
}
