import { useEffect, useMemo, useRef, type ReactNode } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { MoreHorizontal, Trash2 } from 'lucide-react';
import type { Scope } from '@/shared/api/scope';
import { failureReason, isNotFoundError } from '@/shared/api/apiError';
import { dateLocale, useLocale, useT, type TranslationKey } from '@/shared/i18n';
import { ageOf } from '@/shared/lib/age';
import { useElementWidth, useNowSeconds } from '@/shared/lib/hooks';
import { rowsCompact } from '@/shared/lib/sizeClass';
import { useSizeClass } from '@/shared/lib/useSizeClass';
import { cn, formatBytes } from '@/shared/lib/utils';
import { ErrorState } from '@/shared/ui/ErrorState';
import { Glass } from '@/shared/ui/Glass';
import { List } from '@/shared/ui/List';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { Menu, MenuItem } from '@/shared/ui/Menu';
import { Page } from '@/shared/ui/Page';
import { ShellLayoutContext, useShellLayout } from '@/shared/ui/shellLayout';
import { Dot, StatusDot } from '@/shared/ui/StatusDot';
import { Toolbar, ToolbarBack } from '@/shared/ui/Toolbar';
import { ContainerRow } from '@/features/containers/components/ContainerRow';
import { useContainerModals } from '@/features/containers/components/useContainerModals';
import { containerPagePath } from '@/features/containers/model';
import { useContainers } from '@/features/containers/queries';
import { ProjectSourceLine } from '../components/ProjectSourceLine';
import { useProjectActions } from '../components/useProjectActions';
import {
  deployProgress,
  isProjectBusy,
  projectFailure,
  projectStatusText,
  projectTone,
  serviceSummary,
  shownSource,
  type ProjectData,
} from '../model';
import { useProject } from '../queries';

interface ProjectPageProps {
  scope: Scope;
  // backTo is the list the back capsule returns to, and backLabel names it.
  backTo: string;
  backLabel: TranslationKey;
  // containersPath is the list a service's container page belongs under.
  containersPath: string;
}

// ProjectPage is a Compose project's page: its services as rows that open
// their containers, and beside them (under them on narrow content) its
// details and a note that deleting it keeps its volumes. A failed project
// leads with its error; a deploying one shows its steps when the API reports
// them. The toolbar holds Start or Stop (Cancel while it deploys) and a menu
// with Delete.
export function ProjectPage({ scope, backTo, backLabel, containersPath }: ProjectPageProps) {
  const t = useT();
  const { id = '' } = useParams<{ id: string }>();
  const location = useLocation();
  const navigate = useNavigate();

  // The list passes the project it was opened from, which is shown until the
  // project's current state arrives.
  const snapshot = location.state?.project as ProjectData | undefined;
  const projectQuery = useProject(scope, id, snapshot);
  const project = isNotFoundError(projectQuery.error) ? undefined : projectQuery.data;

  const back = <ToolbarBack to={backTo} label={t(backLabel)} />;

  if (!project) {
    return (
      <Page toolbar={<Toolbar leading={back} title={snapshot?.name || id} />}>
        {projectQuery.isError ? (
          <ErrorState
            title={t('projects.detailsLoadFailed')}
            message={failureReason(projectQuery.error, t)}
            onRetry={isNotFoundError(projectQuery.error) ? undefined : () => projectQuery.refetch()}
            isRetrying={projectQuery.isFetching}
          />
        ) : (
          <LoadingRows rows={4} dot={false} columns={[1]} />
        )}
      </Page>
    );
  }

  return (
    <ProjectView
      key={project.id}
      scope={scope}
      project={project}
      back={back}
      containersPath={containersPath}
      onDeleted={() => navigate(backTo)}
    />
  );
}

interface ProjectViewProps {
  scope: Scope;
  project: ProjectData;
  back: ReactNode;
  containersPath: string;
  onDeleted: () => void;
}

function ProjectView({ scope, project, back, containersPath, onDeleted }: ProjectViewProps) {
  const t = useT();
  const navigate = useNavigate();
  const phone = useSizeClass() === 'phone';
  const shellLayout = useShellLayout();
  const compact = shellLayout.rowsCompact;
  const actions = useProjectActions(scope, project, onDeleted);
  // Beside the details the services have less room than the page, so their
  // rows turn compact by the width of their own column.
  const [mainRef, mainWidth] = useElementWidth<HTMLDivElement>(window.innerWidth);
  const servicesLayout = useMemo(
    () => ({ ...shellLayout, rowsCompact: compact || rowsCompact(mainWidth) }),
    [shellLayout, compact, mainWidth],
  );
  const { openExpose, modals } = useContainerModals(scope);

  // A deploying, starting or stopping project changes its containers, so the
  // list keeps refreshing even while it is still empty.
  const busy = isProjectBusy(project.status);
  const containersQuery = useContainers(scope, 1, 100, { projectId: project.id, polling: busy });
  const services = containersQuery.data?.items ?? [];

  // Reaching a new status ends that polling, so read the services once more.
  const lastStatus = useRef(project.status);
  const { refetch: refetchServices } = containersQuery;
  useEffect(() => {
    if (lastStatus.current === project.status) return;
    lastStatus.current = project.status;
    void refetchServices();
  }, [project.status, refetchServices]);

  const tone = projectTone(project.status, services);
  const statusLabel = projectStatusText(t, project.status, serviceSummary(services).failing);
  // On the page Cancel deployment fits the capsule as Cancel.
  const control = actions.canCancel ? { ...actions.primary, label: t('projects.cancelShort') } : actions.primary;
  const ControlIcon = control.icon;

  const toolbarActions = (
    <Glass className="inline-flex h-10 shrink-0 items-center gap-0.5 rounded-full p-1">
      <button
        type="button"
        disabled={control.disabled}
        onClick={control.run}
        aria-label={phone ? control.label : undefined}
        title={phone ? control.label : undefined}
        className="inline-flex h-8 items-center gap-1.5 rounded-full px-3 font-semibold text-label transition-colors hover:bg-fill disabled:opacity-50"
      >
        <ControlIcon aria-hidden className="h-[14px] w-[14px]" strokeWidth={2.2} />
        {!phone && control.label}
      </button>
      <Menu
        label={t('projects.actionsOf', { name: project.name })}
        iconOnly
        sheetTitle={project.name}
        trigger={<MoreHorizontal aria-hidden className="h-[18px] w-[18px]" />}
        triggerClassName="flex h-8 w-8 items-center justify-center rounded-full text-label transition-colors hover:bg-fill aria-expanded:bg-fill"
      >
        <MenuItem icon={Trash2} danger disabled={actions.deleteDisabled} onSelect={actions.requestDelete}>{t('common.delete')}</MenuItem>
      </Menu>
    </Glass>
  );

  const failure = project.status === 'failed' && (
    <div
      role="alert"
      className="flex items-start gap-3 rounded-2xl bg-red/10 px-4 py-3 text-red-text shadow-[inset_0_0_0_0.5px_color-mix(in_srgb,var(--red)_35%,transparent)]"
    >
      <Dot tone="bad" className="mt-1.5" />
      <div className="min-w-0">
        <div className="font-semibold">{t('projects.deployFailedTitle')}</div>
        <div className="[overflow-wrap:anywhere]">{projectFailure(t, project) ?? t('projects.failureUnknown')}</div>
      </div>
    </div>
  );

  const progress = busy ? deployProgress(project.deploy_stage) : undefined;

  const servicesSection = (
    <section aria-labelledby="project-services" className="space-y-2">
      <div className="flex items-baseline gap-2 px-0.5">
        <h2 id="project-services" className="text-[13px] font-semibold text-label">{t('projects.services')}</h2>
        {services.length > 0 && <span className="text-[12px] text-label-3">{services.length}</span>}
      </div>
      {containersQuery.isLoading ? (
        <LoadingRows rows={2} columns={servicesLayout.rowsCompact ? [1] : [1.5, 1.2, 1, 0.5, 0.5]} />
      ) : containersQuery.isError ? (
        <ErrorState
          title={t('containers.loadFailed')}
          message={failureReason(containersQuery.error, t)}
          onRetry={() => containersQuery.refetch()}
          isRetrying={containersQuery.isFetching}
        />
      ) : services.length > 0 ? (
        <ShellLayoutContext.Provider value={servicesLayout}>
          <List label={t('projects.servicesOf', { name: project.name })}>
            {services.map((service) => (
              <ContainerRow
                key={service.id}
                container={service}
                onOpen={(opened, view) => navigate(containerPagePath(containersPath, opened.id, view), { state: { container: opened } })}
                onExpose={scope === 'own' ? openExpose : undefined}
              />
            ))}
          </List>
        </ShellLayoutContext.Provider>
      ) : (
        <p className="px-0.5 text-label-2">
          {t(project.status === 'failed' ? 'projects.noServicesFailed' : busy ? 'projects.noServicesYet' : 'projects.noContainers')}
        </p>
      )}
    </section>
  );

  const side = (
    <aside aria-labelledby="project-details" className={cn('space-y-3', !compact && 'min-h-0 overflow-y-auto px-1 pb-14 pt-0.5 [scrollbar-width:thin]')}>
      <h2 id="project-details" className="text-[13px] font-semibold text-label">{t('containers.details')}</h2>
      <ProjectFacts project={project} memoryBytes={serviceSummary(services).memoryBytes} hasServices={services.length > 0} />
      <p className="text-xs leading-snug text-label-2">{t('projects.deleteKeepsVolumes')}</p>
    </aside>
  );

  const main = (
    <div ref={mainRef} className="min-w-0 space-y-6">
      {failure}
      {progress && <DeployProgressList stages={progress} />}
      {servicesSection}
    </div>
  );

  return (
    <Page
      fill={!compact}
      toolbar={
        <Toolbar
          leading={back}
          title={project.name}
          status={<StatusDot tone={tone} className="shrink-0 text-[13px] font-medium text-label-2">{statusLabel}</StatusDot>}
          actions={toolbarActions}
        />
      }
    >
      {compact ? (
        <div className="space-y-8 pb-6">
          {main}
          {side}
        </div>
      ) : (
        <div className="grid h-full grid-cols-[minmax(0,1fr)_clamp(240px,30%,300px)] gap-[18px]">
          <div className="min-h-0 overflow-y-auto pb-14 [scrollbar-width:thin]">{main}</div>
          {side}
        </div>
      )}

      {actions.dialogs}
      {modals}
    </Page>
  );
}

// DeployProgressList shows a deployment's steps, the current one pulsing.
function DeployProgressList({ stages }: { stages: NonNullable<ReturnType<typeof deployProgress>> }) {
  const t = useT();
  return (
    <section aria-labelledby="project-progress" className="space-y-2.5">
      <h2 id="project-progress" className="px-0.5 text-[13px] font-semibold text-label">{t('projects.deploying')}</h2>
      <ol className="space-y-2 px-0.5">
        {stages.map((step) => (
          <li
            key={step.stage}
            aria-current={step.state === 'current' ? 'step' : undefined}
            className={cn('flex items-center gap-2.5', step.state === 'next' ? 'text-label-3' : 'text-label', step.state === 'current' && 'font-semibold')}
          >
            <Dot tone={step.state === 'done' ? 'ok' : step.state === 'current' ? 'busy' : 'idle'} />
            {t(`projects.stage.${step.stage}`)}
            {step.state === 'done' && <span className="sr-only">{t('projects.stepDone')}</span>}
          </li>
        ))}
      </ol>
    </section>
  );
}

// ProjectFacts lists a project's details, label above value: its source and
// Compose file when the API gives them, the memory its services reserve
// (once it has services) and its creation.
function ProjectFacts({ project, memoryBytes, hasServices }: { project: ProjectData; memoryBytes: number; hasServices: boolean }) {
  const t = useT();
  const locale = useLocale();
  const now = useNowSeconds(60_000);
  const age = ageOf(project.created_at, now);

  const facts: [string, ReactNode][] = [];
  const source = shownSource(project.source);
  if (source) facts.push([t('projects.source'), <ProjectSourceLine source={source} />]);
  if (project.compose_file) {
    facts.push([t('projects.composeFile'), <span className="font-mono text-[12px]">{project.compose_file}</span>]);
  }
  if (hasServices) facts.push([t('projects.memoryReserved'), formatBytes(memoryBytes, 1)]);
  facts.push([
    t('projects.column.created'),
    t('containers.createdValue', {
      date: new Date(project.created_at * 1000).toLocaleString(dateLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }),
      age: t(`common.age.${age.unit}`, { count: age.count }),
    }),
  ]);

  return (
    <dl>
      {facts.map(([label, value]) => (
        <div key={label} className="grid gap-[3px] border-t border-separator px-0.5 py-2.5 first:border-t-0 first:pt-0">
          <dt className="text-[12px] font-semibold text-label-2">{label}</dt>
          <dd className="min-w-0 leading-[1.45] [overflow-wrap:anywhere]">{value}</dd>
        </div>
      ))}
    </dl>
  );
}
