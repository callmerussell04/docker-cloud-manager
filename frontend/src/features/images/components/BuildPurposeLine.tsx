import { useT } from '@/shared/i18n';
import type { BuildPurpose } from '../model';

// BuildPurposeLine says what a build was for: a Compose project's service,
// a Git repository with its ref as a chip, or an uploaded archive.
export function BuildPurposeLine({ purpose }: { purpose: BuildPurpose }) {
  const t = useT();
  switch (purpose.kind) {
    case 'service':
      return <span title={purpose.service}>{t('images.purpose.service', { service: purpose.service })}</span>;
    case 'git':
      return (
        <span className="inline-flex min-w-0 max-w-full items-center gap-1.5" title={purpose.ref ? `${purpose.repository} · ${purpose.ref}` : purpose.repository}>
          <span className="truncate">{purpose.repository}</span>
          {purpose.ref && <span className="shrink-0 rounded-md bg-fill px-1.5 font-mono text-[11px] text-label-2">{purpose.ref}</span>}
        </span>
      );
    case 'archive':
      return <span title={purpose.file}>{t('images.purpose.archive', { file: purpose.file })}</span>;
    case 'unknown':
      return <span className="text-label-3">{t('images.purpose.unknown')}</span>;
  }
}
