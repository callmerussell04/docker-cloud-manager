export interface DashboardStats {
  containers_total: number;
  containers_running: number;
  containers_quota: number;
  ram_used_bytes: number;
  ram_quota_bytes: number;
  cpu_used_millicores: number;
  cpu_quota_millicores: number;
  disk_used_mb: number;
  disk_quota_mb: number;
  volumes_total: number;
  volumes_quota: number;
  images_total: number;
  projects_total: number;
}

// QuotaLevel is how close a figure is to its quota: comfortable, high from
// three quarters, full from nine tenths.
export type QuotaLevel = 'ok' | 'high' | 'full';

// quotaLevel is the level of a figure against its quota; with no quota it is ok.
export function quotaLevel(used: number, total: number): QuotaLevel {
  const share = total > 0 ? used / total : 0;
  if (share >= 0.9) return 'full';
  if (share >= 0.75) return 'high';
  return 'ok';
}

// MemoryHolder is something that holds part of the user's memory quota: a
// container that runs or is about to, and the reservation it holds.
export interface MemoryHolder {
  id: string;
  bytes: number;
}

// MemoryMapPart is one block of the memory map: a holder, the reservations
// the map has no holder for (the containers beyond the page it read), or the
// part of the quota left free.
export type MemoryMapPart<H extends MemoryHolder = MemoryHolder> =
  | { kind: 'holder'; holder: H; bytes: number }
  | { kind: 'other'; bytes: number }
  | { kind: 'free'; bytes: number };

// memoryMapParts divides the memory quota into the map's blocks: the holders,
// largest first (equal ones by id, so the map holds still between polls),
// then what is reserved by containers the map does not list, then what is
// left free. Together they make the quota, or what is reserved when that
// passes the quota.
export function memoryMapParts<H extends MemoryHolder>(holders: readonly H[], usedBytes: number, quotaBytes: number): MemoryMapPart<H>[] {
  const shown = holders
    .filter((holder) => holder.bytes > 0)
    .sort((a, b) => b.bytes - a.bytes || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  const listed = shown.reduce((sum, holder) => sum + holder.bytes, 0);
  const other = Math.max(usedBytes - listed, 0);
  const free = Math.max(quotaBytes - Math.max(usedBytes, listed), 0);
  const parts: MemoryMapPart<H>[] = shown.map((holder) => ({ kind: 'holder', holder, bytes: holder.bytes }));
  if (other > 0) parts.push({ kind: 'other', bytes: other });
  if (free > 0) parts.push({ kind: 'free', bytes: free });
  return parts;
}

// BlockContent is what a memory map block has room to show: its name above
// its amount, the amount alone, or nothing (its tooltip and accessible name
// still say both).
export type BlockContent = 'name' | 'amount' | 'none';

// The block's padding and border across, the width of a character of its
// name and amount, the most characters of a name worth keeping before it
// truncates, and the heights two lines and one line need.
const blockInset = 30;
const nameCharWidth = 7.5;
const readableNameChars = 14;
const amountWidth = 48;
const twoLinesHeight = 56;
const oneLineHeight = 34;

// blockContent decides what a block of the given size in pixels shows, so a
// small block shows its amount rather than a name cut to a few letters.
export function blockContent(width: number, height: number, name: string): BlockContent {
  const inner = width - blockInset;
  if (inner < amountWidth || height < oneLineHeight) return 'none';
  const nameWidth = Math.min(name.length, readableNameChars) * nameCharWidth;
  return inner >= nameWidth && height >= twoLinesHeight ? 'name' : 'amount';
}

// seriesCount is how many chart series tokens there are (series-1 to -5).
const seriesCount = 5;

// projectSeries gives each Compose project the chart series (1 to 5) its
// containers are coloured with on the memory map: by the order of the
// projects' ids, so a project keeps its colour while others start and stop.
// Past the fifth project the series repeat; the legend names each one.
export function projectSeries(projectIds: readonly string[]): Map<string, number> {
  const ordered = [...new Set(projectIds)].sort();
  return new Map(ordered.map((id, index) => [id, (index % seriesCount) + 1]));
}
