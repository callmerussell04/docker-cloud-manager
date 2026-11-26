import { Suspense, useState, type ReactNode } from 'react';
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { Globe, MoreHorizontal, Play, Square, Trash2 } from 'lucide-react';
import type { Scope } from '@/shared/api/scope';
import { failureReason, getServerErrorMessage, isNotFoundError } from '@/shared/api/apiError';
import { useT, type TranslationKey } from '@/shared/i18n';
import { useSizeClass } from '@/shared/lib/useSizeClass';
import { ConfirmDialog } from '@/shared/ui/ConfirmDialog';
import { Console, ConsoleNote } from '@/shared/ui/Console';
import { ErrorState } from '@/shared/ui/ErrorState';
import { Glass } from '@/shared/ui/Glass';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { Menu, MenuItem, MenuSeparator } from '@/shared/ui/Menu';
import { Page } from '@/shared/ui/Page';
import { Pills } from '@/shared/ui/Pills';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Dot, StatusDot } from '@/shared/ui/StatusDot';
import { Toolbar, ToolbarBack } from '@/shared/ui/Toolbar';
import {
  appendUsageSample,
  blockedContainerActions,
  consoleViewOf,
  consoleViewParam,
  containerStatusLabel,
  containerTone,
  type ConsoleView,
  type ContainerData,
  type ContainerStats,
  type UsageSample,
} from '../model';
import { ContainerFacts } from '../components/ContainerFacts';
import { ContainerUsage } from '../components/ContainerUsage';
import { useExposeDialog } from '../components/useExposeDialog';
import { useContainer, useContainerAction, useContainerStats } from '../queries';
import { ContainerLogs } from '../streams/ContainerLogs';
import { LazyContainerTerminal } from '../streams/LazyContainerTerminal';

interface ContainerDetailsPageProps {
  scope: Scope;
  // backTo is the list the back link returns to. The route supplies it, so
  // this page does not know the admin area's URLs.
  backTo: string;
  // backLabel names that list on the back capsule.
  backLabel: TranslationKey;
  // projectName names a Compose project by its ID; the route joins it in,
  // as this feature does not read projects.
  projectName?: (projectId: string) => string | undefined;
}

// useUsageHistory keeps the stats samples taken while the page is open,
// starting empty: one sample per answer the stats poll brings, none from an
// answer cached by an earlier visit, and none while the container is not
// running.
function useUsageHistory(stats: ContainerStats | undefined, updatedAt: number, running: boolean) {
  const [record, setRecord] = useState<{ at: number; samples: UsageSample[] }>(() => ({ at: updatedAt, samples: [] }));
  // Adjusting state while rendering, as React documents for state that
  // follows a changed input; React re-renders before committing.
  if (!running) {
    if (record.samples.length > 0) setRecord({ at: updatedAt, samples: [] });
  } else if (stats && updatedAt !== record.at) {
    setRecord({ at: updatedAt, samples: appendUsageSample(record.samples, { cpu: stats.cpu_percentage, memory: stats.memory_usage_bytes }) });
  }
  return record.samples;
}

// ContainerDetailsPage is a container's page, console first: its live log, or
// its terminal, fills most of the page, switched by the Logs and Terminal
// pills (kept in the URL as ?view=terminal). Beside it, or under it on narrow
// content, a column holds its usage and details. A failed container leads
// with its error. Only the console shown has a stream open.
export function ContainerDetailsPage({ scope, backTo, backLabel, projectName }: ContainerDetailsPageProps) {
  const t = useT();
  const { id = '' } = useParams<{ id: string }>();
  const location = useLocation();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const phone = useSizeClass() === 'phone';
  const compact = useShellLayout().rowsCompact;
  const view = consoleViewOf(params);

  // The list passes the row it was opened from, which is shown until the
  // container's current state arrives.
  const snapshot = location.state?.container as ContainerData | undefined;
  const containerQuery = useContainer(scope, id, snapshot);
  // A container that is gone is not shown from the last answer that had it.
  const container = isNotFoundError(containerQuery.error) ? undefined : containerQuery.data;
  const running = container?.status === 'running';

  const statsQuery = useContainerStats(scope, id, running);
  // Stats kept from an earlier run are not those of a stopped container.
  const stats = running ? statsQuery.data : undefined;
  const history = useUsageHistory(stats, statsQuery.dataUpdatedAt, running);

  const actionMutation = useContainerAction(scope);
  const { openExpose, exposeDialog } = useExposeDialog(scope);
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);

  const changeView = (next: ConsoleView) =>
    setParams(
      (current) => {
        const updated = new URLSearchParams(current);
        if (next === 'logs') updated.delete(consoleViewParam);
        else updated.set(consoleViewParam, next);
        return updated;
      },
      { replace: true, state: location.state },
    );

  const back = <ToolbarBack to={backTo} label={t(backLabel)} />;

  if (!container) {
    return (
      <Page toolbar={<Toolbar leading={back} title={snapshot?.name || id} />}>
        {containerQuery.isError ? (
          <ErrorState
            title={t('containers.detailsLoadFailed')}
            error={containerQuery.error}
            onRetry={isNotFoundError(containerQuery.error) ? undefined : () => containerQuery.refetch()}
            isRetrying={containerQuery.isFetching}
          />
        ) : (
          <LoadingRows rows={4} dot={false} columns={[1]} />
        )}
      </Page>
    );
  }

  const tone = containerTone(container.status);
  const statusLabel = containerStatusLabel(t, container.status);
  const blocked = blockedContainerActions(container.status);
  const busy = actionMutation.isPending;
  const toggle = running
    ? { icon: Square, label: t('containers.stop'), disabled: blocked.stop || busy, action: 'stop' as const }
    : { icon: Play, label: t('containers.start'), disabled: blocked.start || busy, action: 'start' as const };
  const ToggleIcon = toggle.icon;

  const actions = (
    <Glass className="inline-flex h-10 shrink-0 items-center gap-0.5 rounded-full p-1">
      <button
        type="button"
        disabled={toggle.disabled}
        onClick={() => actionMutation.mutate({ id: container.id, action: toggle.action })}
        aria-label={phone ? toggle.label : undefined}
        title={phone ? toggle.label : undefined}
        className="inline-flex h-8 items-center gap-1.5 rounded-full px-3 font-semibold text-label transition-colors hover:bg-fill disabled:opacity-50"
      >
        <ToggleIcon aria-hidden className="h-[14px] w-[14px]" strokeWidth={2.2} />
        {!phone && toggle.label}
      </button>
      <Menu
        label={t('containers.actionsOf', { name: container.name })}
        iconOnly
        sheetTitle={container.name}
        trigger={<MoreHorizontal aria-hidden className="h-[18px] w-[18px]" />}
        triggerClassName="flex h-8 w-8 items-center justify-center rounded-full text-label transition-colors hover:bg-fill aria-expanded:bg-fill"
      >
        {scope === 'own' && (
          <>
            <MenuItem icon={Globe} disabled={blocked.expose || busy} onSelect={() => openExpose(container)}>{t('containers.routingSettings')}</MenuItem>
            <MenuSeparator />
          </>
        )}
        <MenuItem icon={Trash2} danger disabled={blocked.delete || busy} onSelect={() => setIsDeleteConfirmOpen(true)}>{t('common.delete')}</MenuItem>
      </Menu>
    </Glass>
  );

  const failure = tone === 'bad' && (
    <div
      role="alert"
      className="flex items-start gap-3 rounded-2xl bg-red-soft px-4 py-3 text-red-text shadow-[inset_0_0_0_0.5px_color-mix(in_srgb,var(--red)_35%,transparent)]"
    >
      <Dot tone="bad" className="mt-1.5" />
      <div className="min-w-0">
        <div className="font-semibold">{statusLabel}</div>
        <div className="[overflow-wrap:anywhere]">
          {getServerErrorMessage(container.last_error, t) ??
            t(container.status === 'missing' ? 'containers.missingNote' : 'containers.failureUnknown')}
        </div>
      </div>
    </div>
  );

  const consoleView: ReactNode =
    view === 'logs' ? (
      <ContainerLogs key={container.id} scope={scope} containerId={container.id} name={container.name} running={running} />
    ) : running ? (
      <Suspense
        fallback={
          <Console>
            <ConsoleNote>{t('containers.consoleConnecting')}</ConsoleNote>
          </Console>
        }
      >
        <LazyContainerTerminal key={container.id} scope={scope} containerId={container.id} />
      </Suspense>
    ) : (
      <Console>
        <ConsoleNote>{t('containers.terminalNotRunning')}</ConsoleNote>
      </Console>
    );

  const side = (
    <>
      <section aria-labelledby="container-usage" className="space-y-3">
        <h2 id="container-usage" className="text-[13px] font-semibold text-label">{t('containers.usage')}</h2>
        <ContainerUsage
          running={running}
          stats={stats}
          history={history}
          loading={statsQuery.isLoading}
          error={
            running && statsQuery.isError && !stats
              ? { message: failureReason(statsQuery.error, t), retry: () => statsQuery.refetch(), retrying: statsQuery.isFetching }
              : undefined
          }
        />
      </section>
      <section aria-labelledby="container-details" className="space-y-3">
        <h2 id="container-details" className="text-[13px] font-semibold text-label">{t('containers.details')}</h2>
        <ContainerFacts
          container={container}
          projectName={container.project_id ? projectName?.(container.project_id) : undefined}
        />
      </section>
    </>
  );

  return (
    <Page
      fill={!compact}
      toolbar={
        <Toolbar
          leading={back}
          title={container.name}
          status={<StatusDot tone={tone} className="shrink-0 text-[13px] font-medium text-label-2">{statusLabel}</StatusDot>}
          pills={
            <Pills
              label={t('containers.consoleLabel')}
              value={view}
              onChange={changeView}
              items={[
                { id: 'logs', label: t('containers.logs') },
                { id: 'terminal', label: t('containers.terminal') },
              ]}
            />
          }
          actions={actions}
        />
      }
    >
      {compact ? (
        <div className="space-y-6">
          {failure}
          <div className="flex h-[62vh] min-h-[320px]">{consoleView}</div>
          {side}
        </div>
      ) : (
        <div className="grid h-full grid-cols-[minmax(0,1fr)_clamp(240px,30%,300px)] gap-[18px]">
          <div className="flex min-h-0 flex-col gap-3">
            {failure}
            {consoleView}
          </div>
          <aside className="min-h-0 space-y-6 overflow-y-auto px-1 pb-14 pt-0.5 [scrollbar-width:thin]">{side}</aside>
        </div>
      )}

      {exposeDialog}

      <ConfirmDialog
        isOpen={isDeleteConfirmOpen}
        title={t('containers.deleteConfirm', { name: container.name })}
        message={t('containers.deleteConsequence')}
        confirmLabel={t('common.delete')}
        isLoading={busy}
        onCancel={() => setIsDeleteConfirmOpen(false)}
        onConfirm={() => actionMutation.mutate({ id: container.id, action: 'delete' }, { onSuccess: () => navigate(backTo) })}
      />
    </Page>
  );
}
