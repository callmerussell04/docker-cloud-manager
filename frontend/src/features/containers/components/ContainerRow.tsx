import { useState } from 'react';
import { ExternalLink, Globe, Layers, Play, ScrollText, Square, Terminal, Trash2 } from 'lucide-react';
import { getServerErrorMessage } from '@/shared/api/apiError';
import type { Scope } from '@/shared/api/scope';
import { useT } from '@/shared/i18n';
import { ageOf } from '@/shared/lib/age';
import { BASE_DOMAIN } from '@/shared/lib/config';
import { useNowSeconds } from '@/shared/lib/hooks';
import { cn, formatBytes } from '@/shared/lib/utils';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { ListRow, RowEnd, RowMenu, RowQuickAction } from '@/shared/ui/List';
import { MenuItem, MenuSeparator } from '@/shared/ui/Menu';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Dot } from '@/shared/ui/StatusDot';
import { blockedContainerActions, containerStatusLabel, containerTone, type ConsoleView, type ContainerData } from '../model';
import { useContainerAction } from '../queries';
import { containerListLayout } from './listLayout';
import { useAutoStopLabel } from './useAutoStopLabel';

interface ContainerRowProps {
  container: ContainerData;
  // project names the Compose project the container belongs to, shown as a
  // tag beside its name; leave it out where the project is already known.
  project?: string;
  // onOpen opens the container's page, on its logs or, when asked, its
  // terminal.
  onOpen: (container: ContainerData, view?: ConsoleView) => void;
  // onExpose opens routing settings; only the user's own containers have it.
  onExpose?: (container: ContainerData) => void;
  // scope is whose list the row is in: the user's own (the default) or every
  // user's, which the admin view shows with each row's owner.
  scope?: Scope;
  // owner names the row's owner, in a column after the name; given only in
  // the admin view.
  owner?: string;
}

// ContainerRow is a container's line in a list: its name and image (and its
// project), its status with the failure or auto-stop after it, its address,
// its memory reservation and its age. The row opens the container; its menu
// holds every action, and a wide window shows logs, terminal and start or
// stop on the hovered row. Compact rows are the name, then status and age.
export function ContainerRow({ container, project, onOpen, onExpose, scope = 'own', owner }: ContainerRowProps) {
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const now = useNowSeconds(60_000);
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);
  const actionMutation = useContainerAction(scope);
  const autoStop = useAutoStopLabel(container.status, container.ttl_deadline);

  const running = container.status === 'running';
  const blocked = blockedContainerActions(container.status);
  const busy = actionMutation.isPending;
  const failure = getServerErrorMessage(container.last_error, t);
  const statusLabel = containerStatusLabel(t, container.status);
  const age = ageOf(container.created_at, now);
  const ageLabel = t(`common.age.${age.unit}`, { count: age.count });
  const toggle = running
    ? { icon: Square, label: t('containers.stop'), disabled: blocked.stop || busy, run: () => actionMutation.mutate({ id: container.id, action: 'stop' }) }
    : { icon: Play, label: t('containers.start'), disabled: blocked.start || busy, run: () => actionMutation.mutate({ id: container.id, action: 'start' }) };

  const detail = failure ? (
    <span className="text-red-text">{failure}</span>
  ) : autoStop ? (
    <span className="text-label-2">{autoStop}</span>
  ) : null;

  const title = (
    <div className="flex min-w-0 items-center gap-2">
      <span className={cn('truncate font-semibold', compact ? 'text-[15px]' : 'text-[13.5px]')} title={container.name}>
        {container.name}
      </span>
      {project !== undefined && (
        <span
          title={t('containers.projectTag', { name: project })}
          className="inline-flex h-[18px] max-w-[45%] shrink-0 items-center gap-1 rounded-full bg-fill px-[7px] text-[11px] font-semibold text-label-2"
        >
          <Layers aria-hidden className="h-[11px] w-[11px] shrink-0" />
          <span className="truncate">{project}</span>
        </span>
      )}
    </div>
  );

  const address = container.domain_prefix ? `${container.domain_prefix}.${BASE_DOMAIN}` : '';

  const menu = (
    <RowMenu label={t('containers.rowActions', { name: container.name })} sheetTitle={container.name}>
      <MenuItem icon={ScrollText} onSelect={() => onOpen(container, 'logs')}>{t('containers.logs')}</MenuItem>
      <MenuItem icon={Terminal} disabled={blocked.terminal} onSelect={() => onOpen(container, 'terminal')}>{t('containers.terminal')}</MenuItem>
      <MenuItem icon={toggle.icon} disabled={toggle.disabled} onSelect={toggle.run}>{toggle.label}</MenuItem>
      {address && (
        <MenuItem icon={ExternalLink} onSelect={() => window.open(`http://${address}`, '_blank', 'noopener,noreferrer')}>{t('containers.openAddress')}</MenuItem>
      )}
      {onExpose && (
        <MenuItem icon={Globe} disabled={blocked.expose || busy} onSelect={() => onExpose(container)}>{t('containers.routingSettings')}</MenuItem>
      )}
      <MenuSeparator />
      <MenuItem icon={Trash2} danger disabled={blocked.delete || busy} onSelect={() => setIsDeleteConfirmOpen(true)}>{t('common.delete')}</MenuItem>
    </RowMenu>
  );

  const quick = (
    <>
      <RowQuickAction icon={ScrollText} label={t('containers.logs')} onClick={() => onOpen(container, 'logs')} />
      <RowQuickAction icon={Terminal} label={t('containers.terminal')} disabled={blocked.terminal} onClick={() => onOpen(container, 'terminal')} />
      <RowQuickAction icon={toggle.icon} label={toggle.label} disabled={toggle.disabled} onClick={toggle.run} />
    </>
  );

  const confirm = (
    <ConfirmDialog
      isOpen={isDeleteConfirmOpen}
      title={t('containers.deleteConfirm', { name: container.name })}
      message={t('containers.deleteConsequence')}
      confirmLabel={t('common.delete')}
      isLoading={busy}
      onCancel={() => setIsDeleteConfirmOpen(false)}
      onConfirm={() => actionMutation.mutate({ id: container.id, action: 'delete' }, { onSuccess: () => setIsDeleteConfirmOpen(false) })}
    />
  );

  return (
    <ListRow template={compact ? containerListLayout.compact : owner !== undefined ? containerListLayout.owned : containerListLayout.wide} onOpen={() => onOpen(container)}>
      <div className="flex min-w-0 items-center gap-3.5">
        <Dot tone={containerTone(container.status)} />
        <div className="min-w-0 flex-1">
          {title}
          {compact ? (
            <div className="mt-0.5 truncate text-[12.5px] text-label-2">
              <span className="text-label">{statusLabel}</span>
              {detail && <> · {detail}</>}
              {owner && ` · ${owner}`}
              {' · '}
              {ageLabel}
            </div>
          ) : (
            <div className="truncate font-mono text-[11.5px] text-label-3" title={container.image_tag}>{container.image_tag}</div>
          )}
        </div>
      </div>
      {!compact && (
        <>
          {owner !== undefined && <div className="truncate text-label-2" title={owner}>{owner}</div>}
          <div className="truncate" title={failure || autoStop || statusLabel}>
            {statusLabel}
            {detail && <> · {detail}</>}
          </div>
          <div className="min-w-0 truncate">
            {address && (
              <a
                href={`http://${address}`}
                target="_blank"
                rel="noreferrer"
                title={t('containers.addressTitle', { address, port: container.internal_port })}
                onClick={(event) => event.stopPropagation()}
                className="font-medium text-accent hover:underline"
              >
                {address}
              </a>
            )}
          </div>
          <div className={cn('tabular-nums', !running && 'text-label-3')}>{formatBytes(container.memory_reservation_bytes, 1)}</div>
        </>
      )}
      <RowEnd age={ageLabel} quick={quick} menu={<>{menu}{confirm}</>} />
    </ListRow>
  );
}
