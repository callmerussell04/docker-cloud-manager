import { Trash2 } from 'lucide-react';
import type { Scope } from '@/shared/api/scope';
import { useT } from '@/shared/i18n';
import { ageOf } from '@/shared/lib/age';
import { BASE_DOMAIN } from '@/shared/lib/config';
import { useNowSeconds } from '@/shared/lib/hooks';
import { cn, formatBytes } from '@/shared/lib/utils';
import { ListRow, RowEnd, RowMenu, RowQuickAction } from '@/shared/ui/List';
import { MenuItem, MenuSeparator } from '@/shared/ui/Menu';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Dot } from '@/shared/ui/StatusDot';
import { containerTone } from '@/features/containers/model';
import { projectSummary, projectTone, serviceSummary, shownSource, type ProjectListItem } from '../model';
import { projectListLayout } from './listLayout';
import { ProjectSourceLine } from './ProjectSourceLine';
import { useProjectActions } from './useProjectActions';

interface ProjectRowProps {
  item: ProjectListItem;
  onOpen: () => void;
  // scope is whose list the row is in: the user's own (the default) or every
  // user's, which the admin view shows with each row's owner.
  scope?: Scope;
  // owner names the row's owner, in a column after the name; given only in
  // the admin view.
  owner?: string;
}

// ProjectRow is a project's line in the list: its name and source (when the
// API gives it), a dot per service with how many run (or the failure, or the
// step a deployment is on), its first address and how many more, the memory
// its services reserve, and its age. The row opens the project; its menu
// holds Start or Stop, Cancel deployment while that is possible, and Delete.
// Compact rows are the name, then the state line and age.
export function ProjectRow({ item, onOpen, scope = 'own', owner }: ProjectRowProps) {
  const { project, services } = item;
  const t = useT();
  const compact = useShellLayout().rowsCompact;
  const now = useNowSeconds(60_000);
  const actions = useProjectActions(scope, project);

  const summary = serviceSummary(services);
  const state = projectSummary(t, project, services);
  const age = ageOf(project.created_at, now);
  const ageLabel = t(`common.age.${age.unit}`, { count: age.count });
  const address = summary.addresses[0] ? `${summary.addresses[0]}.${BASE_DOMAIN}` : '';
  const source = shownSource(project.source);

  const stateLine = <span className={cn(state.bad && 'text-red-text')}>{state.text}</span>;

  const menu = (
    <RowMenu label={t('projects.rowActions', { name: project.name })} sheetTitle={project.name}>
      <MenuItem icon={actions.primary.icon} disabled={actions.primary.disabled} onSelect={actions.primary.run}>{actions.primary.label}</MenuItem>
      <MenuSeparator />
      <MenuItem icon={Trash2} danger disabled={actions.deleteDisabled} onSelect={actions.requestDelete}>{t('common.delete')}</MenuItem>
    </RowMenu>
  );

  const quick = (
    <RowQuickAction icon={actions.primary.icon} label={actions.primary.label} disabled={actions.primary.disabled} onClick={actions.primary.run} />
  );

  return (
    <ListRow template={compact ? projectListLayout.compact : owner !== undefined ? projectListLayout.owned : projectListLayout.wide} onOpen={onOpen}>
      <div className="flex min-w-0 items-center gap-3.5">
        <Dot tone={projectTone(project.status, services)} />
        <div className="min-w-0 flex-1">
          <div className={cn('truncate font-semibold', compact ? 'text-[15px]' : 'text-[13.5px]')} title={project.name}>
            {project.name}
          </div>
          {compact ? (
            <div className="mt-0.5 truncate text-[12.5px] text-label-2">
              {stateLine}
              {owner && ` · ${owner}`}
              {' · '}
              {ageLabel}
            </div>
          ) : (
            source && (
              <div className="min-w-0 text-[11.5px] text-label-3">
                <ProjectSourceLine source={source} />
              </div>
            )
          )}
        </div>
      </div>
      {!compact && (
        <>
          {owner !== undefined && <div className="truncate text-label-2" title={owner}>{owner}</div>}
          <div className="flex min-w-0 items-center gap-2">
            {services.length > 0 && (
              <span aria-hidden className="flex shrink-0 items-center gap-[3px]">
                {services.map((service) => <Dot key={service.id} tone={containerTone(service.status)} />)}
              </span>
            )}
            <span className="truncate" title={state.text}>{stateLine}</span>
          </div>
          <div className="min-w-0 truncate">
            {address && (
              <>
                <a
                  href={`http://${address}`}
                  target="_blank"
                  rel="noreferrer"
                  title={address}
                  onClick={(event) => event.stopPropagation()}
                  className="font-medium text-accent hover:underline"
                >
                  {address}
                </a>
                {summary.addresses.length > 1 && (
                  <span className="text-label-3"> {t('projects.moreAddresses', { count: summary.addresses.length - 1 })}</span>
                )}
              </>
            )}
          </div>
          <div className={cn('tabular-nums', project.status !== 'running' && 'text-label-3')}>
            {summary.total > 0 ? formatBytes(summary.memoryBytes, 1) : ''}
          </div>
        </>
      )}
      <RowEnd age={ageLabel} quick={quick} menu={<>{menu}{actions.dialogs}</>} />
    </ListRow>
  );
}
