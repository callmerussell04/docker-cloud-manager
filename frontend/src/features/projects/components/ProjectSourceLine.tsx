import { FileCode, FolderArchive, GitBranch } from 'lucide-react';
import type { ProjectSource } from '../model';

// ProjectSourceLine names where a project came from: the uploaded archive or
// YAML file, or the Git repository with its ref as a chip. A file the API
// does not name shows nothing.
export function ProjectSourceLine({ source }: { source: ProjectSource }) {
  if (source.kind === 'git') {
    return (
      <span className="flex min-w-0 items-center gap-1.5">
        <GitBranch aria-hidden className="h-[13px] w-[13px] shrink-0" />
        <span className="truncate" title={source.repo_url}>{source.repo_url.replace(/^https?:\/\//, '').replace(/\.git$/, '')}</span>
        {source.ref && (
          <span className="inline-flex h-[18px] shrink-0 items-center rounded-full bg-fill px-[7px] font-mono text-[11px] text-label-2">{source.ref}</span>
        )}
      </span>
    );
  }
  if (!source.file) return null;
  const Icon = source.kind === 'archive' ? FolderArchive : FileCode;
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <Icon aria-hidden className="h-[13px] w-[13px] shrink-0" />
      <span className="truncate" title={source.file}>{source.file}</span>
    </span>
  );
}
