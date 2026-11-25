import { FileText, Trash2, XCircle } from 'lucide-react';
import type { Scope } from '@/shared/api/scope';
import { useT } from '@/shared/i18n';
import { ageOf } from '@/shared/lib/age';
import { useNowSeconds } from '@/shared/lib/hooks';
import { cn } from '@/shared/lib/utils';
import { ListRow, RowEnd, RowMenu, RowQuickAction } from '@/shared/ui/List';
import { MenuItem, MenuSeparator } from '@/shared/ui/Menu';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Dot } from '@/shared/ui/StatusDot';
import { buildPurpose, buildStatusLabel, buildTiming, buildTone, formatDuration, isBuildActive, type BuildData } from '../model';
import { BuildPurposeLine } from './BuildPurposeLine';
import { buildListLayout } from './listLayout';
import { useBuildActions } from './useBuildActions';

interface BuildRowProps {
  build: BuildData;
  onOpen: () => void;
  // scope is whose list the row is in: the user's own (the default) or every
  // user's, which the admin view shows with each row's owner.
  scope?: Scope;
  // owner names the row's owner, in a column after the name; given only in
  // the admin view.
  owner?: string;
}

// BuildRow is a build's line in the list: its tag, what it was for, its
// state with a live timer while it is queued or running, how long it took
// and when it was queued. The row opens the build's page; its menu holds the
// log, Cancel while it is active and Delete once it has finished. Compact
// rows are the tag, then the state and age.
export function BuildRow({ build, onOpen, scope = 'own', owner }: BuildRowProps) {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const active = isBuildActive(build.status);
  // A queued or running build counts its time live.
  const now = useNowSeconds(active ? 1_000 : 60_000);
  const actions = useBuildActions(scope, build);

  const tone = buildTone(build.status);
  const timing = buildTiming(build, now);
  const statusText = timing.phase === 'queued' || timing.phase === 'running'
    ? `${buildStatusLabel(t, build.status)} · ${formatDuration(t, timing.seconds)}`
    : buildStatusLabel(t, build.status);
  const took = timing.phase === 'finished' ? formatDuration(t, timing.seconds) : '';
  const age = ageOf(build.created_at, now);
  const ageLabel = t(`common.age.${age.unit}`, { count: age.count });
  const name = build.tag || build.id;
  const status = <span className={cn('tabular-nums', tone === 'bad' && 'text-red-text')}>{statusText}</span>;

  const menu = (
    <RowMenu label={t('images.buildRowActions', { name })} sheetTitle={name}>
      <MenuItem icon={FileText} onSelect={onOpen}>{t('images.openLog')}</MenuItem>
      <MenuSeparator />
      {actions.canCancel ? (
        <MenuItem icon={XCircle} disabled={actions.cancelPending} onSelect={actions.requestCancel}>{t('images.cancelBuild')}</MenuItem>
      ) : (
        <MenuItem icon={Trash2} danger disabled={actions.deletePending} onSelect={actions.requestDelete}>{t('common.delete')}</MenuItem>
      )}
    </RowMenu>
  );

  const quick = (
    <>
      <RowQuickAction icon={FileText} label={t('images.openLog')} onClick={onOpen} />
      {actions.canCancel && <RowQuickAction icon={XCircle} label={t('images.cancelBuild')} disabled={actions.cancelPending} onClick={actions.requestCancel} />}
    </>
  );

  return (
    <ListRow template={compact ? buildListLayout.compact : owner !== undefined ? buildListLayout.owned : buildListLayout.wide} onOpen={onOpen}>
      <div className="flex min-w-0 items-center gap-3.5">
        <Dot tone={tone} />
        <div className="min-w-0 flex-1">
          <div className={cn('truncate font-semibold', compact ? 'text-[15px]' : 'text-[13.5px]')} title={name}>{name}</div>
          {compact && (
            <div className="mt-0.5 truncate text-[12.5px] text-label-2">
              {status}
              {owner && ` · ${owner}`}
              {' · '}
              {ageLabel}
            </div>
          )}
        </div>
      </div>
      {!compact && (
        <>
          {owner !== undefined && <div className="truncate text-label-2" title={owner}>{owner}</div>}
          <div className="min-w-0 truncate"><BuildPurposeLine purpose={buildPurpose(build)} /></div>
          <div className="truncate" title={statusText}>{status}</div>
          <div className="truncate tabular-nums text-label-2">{took}</div>
        </>
      )}
      <RowEnd age={ageLabel} quick={quick} menu={<>{menu}{actions.dialogs}</>} />
    </ListRow>
  );
}
