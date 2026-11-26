import type { ReactNode } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { MoreHorizontal, Trash2, XCircle } from 'lucide-react';
import type { Scope } from '@/shared/api/scope';
import { isNotFoundError } from '@/shared/api/apiError';
import { dateLocale, useLocale, useT, type TranslationKey } from '@/shared/i18n';
import { ageOf } from '@/shared/lib/age';
import { useNowSeconds } from '@/shared/lib/hooks';
import { useSizeClass } from '@/shared/lib/useSizeClass';
import { CopyButton } from '@/shared/ui/CopyButton';
import { ErrorState } from '@/shared/ui/ErrorState';
import { Glass } from '@/shared/ui/Glass';
import { LoadingRows } from '@/shared/ui/LoadingRows';
import { Menu, MenuItem } from '@/shared/ui/Menu';
import { Page } from '@/shared/ui/Page';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { Dot, StatusDot } from '@/shared/ui/StatusDot';
import { Toolbar, ToolbarBack } from '@/shared/ui/Toolbar';
import { BuildLog } from '../components/BuildLog';
import { BuildPurposeLine } from '../components/BuildPurposeLine';
import { useBuildActions } from '../components/useBuildActions';
import { buildPurpose, buildStatusLabel, buildTiming, buildTone, formatDuration, type BuildData } from '../model';
import { useBuild } from '../queries';

interface BuildPageProps {
  scope: Scope;
  // backTo is the list the back capsule returns to, and backLabel names it.
  backTo: string;
  backLabel: TranslationKey;
}

// BuildPage is a build's page, laid out as a container's: its log fills the
// page and its details sit in a column beside it (under it on narrow
// content). A failed build leads with what failed. The toolbar holds Cancel
// while the build is queued or running, and a menu with Delete once it has
// finished.
export function BuildPage({ scope, backTo, backLabel }: BuildPageProps) {
  const t = useT();
  const { id = '' } = useParams<{ id: string }>();
  const location = useLocation();
  const navigate = useNavigate();

  // The list passes the build it was opened from, which is shown until the
  // build's current state arrives.
  const snapshot = location.state?.build as BuildData | undefined;
  const buildQuery = useBuild(scope, id, snapshot);
  const build = isNotFoundError(buildQuery.error) ? undefined : buildQuery.data;
  const back = <ToolbarBack to={backTo} label={t(backLabel)} />;

  if (!build) {
    return (
      <Page toolbar={<Toolbar leading={back} title={snapshot?.tag || id} />}>
        {buildQuery.isError ? (
          <ErrorState
            title={t('images.buildLoadFailed')}
            error={buildQuery.error}
            onRetry={isNotFoundError(buildQuery.error) ? undefined : () => buildQuery.refetch()}
            isRetrying={buildQuery.isFetching}
          />
        ) : (
          <LoadingRows rows={4} dot={false} columns={[1]} />
        )}
      </Page>
    );
  }

  return <BuildView key={build.id} scope={scope} build={build} back={back} onDeleted={() => navigate(backTo)} />;
}

interface BuildViewProps {
  scope: Scope;
  build: BuildData;
  back: ReactNode;
  onDeleted: () => void;
}

function BuildView({ scope, build, back, onDeleted }: BuildViewProps) {
  const t = useT();
  const phone = useSizeClass() === 'phone';
  const compact = useShellLayout().rowsCompact;
  const actions = useBuildActions(scope, build, onDeleted);
  const tone = buildTone(build.status);
  const statusLabel = buildStatusLabel(t, build.status);
  const name = build.tag || build.id;

  const toolbarActions = (
    <Glass className="inline-flex h-10 shrink-0 items-center gap-0.5 rounded-full p-1">
      {actions.canCancel && (
        <button
          type="button"
          disabled={actions.cancelPending}
          onClick={actions.requestCancel}
          aria-label={phone ? t('images.cancelBuild') : undefined}
          title={phone ? t('images.cancelBuild') : undefined}
          className="inline-flex h-8 items-center gap-1.5 rounded-full px-3 font-semibold text-label transition-colors hover:bg-fill disabled:opacity-50"
        >
          <XCircle aria-hidden className="h-[14px] w-[14px]" strokeWidth={2.2} />
          {!phone && t('images.cancelShort')}
        </button>
      )}
      <Menu
        label={t('images.buildRowActions', { name })}
        iconOnly
        sheetTitle={name}
        trigger={<MoreHorizontal aria-hidden className="h-[18px] w-[18px]" />}
        triggerClassName="flex h-8 w-8 items-center justify-center rounded-full text-label transition-colors hover:bg-fill aria-expanded:bg-fill"
      >
        <MenuItem
          icon={Trash2}
          danger
          disabled={!actions.canDelete || actions.deletePending}
          title={actions.canDelete ? undefined : t('images.deleteBuildBlocked')}
          onSelect={actions.requestDelete}
        >
          {t('common.delete')}
        </MenuItem>
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
        <div className="font-semibold">{t('images.buildFailedTitle')}</div>
        <div className="[overflow-wrap:anywhere]">{t(failureLineKey(build.status))}</div>
      </div>
    </div>
  );

  const side = (
    <section aria-labelledby="build-details" className="space-y-3">
      <h2 id="build-details" className="text-[13px] font-semibold text-label">{t('containers.details')}</h2>
      <BuildFacts build={build} />
    </section>
  );

  const log = <BuildLog scope={scope} build={build} />;

  return (
    <Page
      fill={!compact}
      toolbar={
        <Toolbar
          leading={back}
          title={name}
          status={<StatusDot tone={tone} className="shrink-0 text-[13px] font-medium text-label-2">{statusLabel}</StatusDot>}
          actions={toolbarActions}
        />
      }
    >
      {compact ? (
        <div className="space-y-6">
          {failure}
          <div className="flex h-[62vh] min-h-[320px]">{log}</div>
          {side}
        </div>
      ) : (
        <div className="grid h-full grid-cols-[minmax(0,1fr)_clamp(240px,30%,300px)] gap-[18px]">
          <div className="flex min-h-0 flex-col gap-3">
            {failure}
            {log}
          </div>
          <aside className="min-h-0 space-y-6 overflow-y-auto px-1 pb-14 pt-0.5 [scrollbar-width:thin]">{side}</aside>
        </div>
      )}
      {actions.dialogs}
    </Page>
  );
}

// failureLineKey says in a line why a build failed, by its status.
function failureLineKey(status: string): TranslationKey {
  switch (status) {
    case 'failed_timeout': return 'images.failure.timeout';
    case 'failed_quota_exceeded': return 'images.failure.quota';
    case 'failed_resource_exhausted': return 'images.failure.resources';
    case 'failed_internal': return 'images.failure.internal';
    default: return 'images.failure.build';
  }
}

// BuildFacts lists a build's details, label above value: its tag with a copy
// button, what it was for, when it was queued, when it started and how long
// it has run or took.
function BuildFacts({ build }: { build: BuildData }) {
  const t = useT();
  const locale = useLocale();
  const now = useNowSeconds(1_000);
  const when = (seconds: number) => {
    const age = ageOf(seconds, now);
    return t('containers.createdValue', {
      date: new Date(seconds * 1000).toLocaleString(dateLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }),
      age: t(`common.age.${age.unit}`, { count: age.count }),
    });
  };
  const timing = buildTiming(build, now);

  const facts: [string, ReactNode][] = [];
  if (build.tag) {
    facts.push([
      t('images.imageTag'),
      <span className="flex items-start gap-1">
        <span className="min-w-0 font-mono text-[12px] [overflow-wrap:anywhere]">{build.tag}</span>
        <CopyButton value={build.tag} />
      </span>,
    ]);
  }
  facts.push([t('images.column.for'), <BuildPurposeLine purpose={buildPurpose(build)} />]);
  facts.push([t('images.queuedAt'), when(build.created_at)]);
  if (build.started_at > 0) facts.push([t('images.startedAt'), when(build.started_at)]);
  if (timing.phase === 'running') facts.push([t('images.runningFor'), formatDuration(t, timing.seconds)]);
  if (timing.phase === 'queued') facts.push([t('images.waitingFor'), formatDuration(t, timing.seconds)]);
  if (timing.phase === 'finished') facts.push([t('images.column.took'), formatDuration(t, timing.seconds)]);

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
