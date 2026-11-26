import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import { ArrowDown, Download, RotateCw, Search, WrapText } from 'lucide-react';
import type { Scope } from '@/shared/api/scope';
import { failureReason } from '@/shared/api/apiError';
import { useT } from '@/shared/i18n';
import { useNowSeconds } from '@/shared/lib/hooks';
import { cn } from '@/shared/lib/utils';
import { Console, ConsoleBar, ConsoleButton, ConsoleNote, ConsoleState } from '@/shared/ui/Console';
import { buildTiming, formatDuration, isBuildActive, isBuildErrorLine, type BuildData } from '../model';
import { useFinishedBuildLog } from '../queries';

// How far from the bottom, in pixels, still counts as reading the newest line.
const latestSlack = 24;

// BuildLog is the build page's log console. Builder stores the log when the
// build ends, so while the build is queued or running the console counts its
// time and says the log comes at the end; then the log opens at its newest
// line, with error lines in red, a filter, wrapping and a download.
export function BuildLog({ scope, build }: { scope: Scope; build: BuildData }) {
  const t = useT();
  const active = isBuildActive(build.status);
  const now = useNowSeconds(active ? 1_000 : 60_000);
  const { data, isLoading, isError, error, refetch, isFetching } = useFinishedBuildLog(scope, build);
  const [filter, setFilter] = useState('');
  const [wrap, setWrap] = useState(true);
  const [following, setFollowing] = useState(true);
  const bodyRef = useRef<HTMLDivElement>(null);

  const lines = useMemo(() => (data ? data.replace(/\n$/, '').split('\n') : []), [data]);
  const shown = useMemo(() => {
    const query = filter.trim().toLowerCase();
    const numbered = lines.map((text, index) => ({ id: index, text, error: isBuildErrorLine(text) }));
    return query ? numbered.filter((line) => line.text.toLowerCase().includes(query)) : numbered;
  }, [lines, filter]);

  useLayoutEffect(() => {
    const body = bodyRef.current;
    if (following && body) body.scrollTop = body.scrollHeight;
  }, [shown, following, wrap]);

  const download = () => {
    const url = URL.createObjectURL(new Blob([data ?? ''], { type: 'text/plain' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = `${(build.tag || build.id).replace(/[^A-Za-z0-9_.-]+/g, '-')}.log`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  };

  const timing = buildTiming(build, now);
  const stateLabel = () => {
    if (build.status === 'pending' && timing.phase === 'queued') return <ConsoleState live={false}>{t('images.logQueued', { duration: formatDuration(t, timing.seconds) })}</ConsoleState>;
    if (build.status === 'running' && timing.phase === 'running') return <ConsoleState live>{t('images.logRunning', { duration: formatDuration(t, timing.seconds) })}</ConsoleState>;
    return <ConsoleState live={false}>{t('images.logFinished')}</ConsoleState>;
  };

  const body = () => {
    if (active) return <ConsoleNote>{t('images.logAtEnd')}</ConsoleNote>;
    if (isLoading) return <ConsoleNote>{t('images.logsLoading')}</ConsoleNote>;
    if (isError) {
      return (
        <ConsoleNote action={<ConsoleButton icon={RotateCw} label={t('common.tryAgain')} disabled={isFetching} onClick={() => refetch()} />}>
          {failureReason(error, t) ?? t('images.loadLogsFailed')}
        </ConsoleNote>
      );
    }
    if (lines.length === 0) return <ConsoleNote>{t('images.logsEmpty')}</ConsoleNote>;
    if (shown.length === 0) return <ConsoleNote>{t('containers.logsNoMatch')}</ConsoleNote>;
    return (
      <div
        ref={bodyRef}
        role="log"
        aria-label={t('images.logLabel', { name: build.tag || build.id })}
        tabIndex={0}
        onScroll={(event) => {
          const target = event.currentTarget;
          setFollowing(target.scrollHeight - target.scrollTop - target.clientHeight <= latestSlack);
        }}
        className="min-h-0 flex-1 overflow-auto px-4 py-3 leading-[1.6] [scrollbar-width:thin] focus-visible:outline-offset-[-2px]"
      >
        {shown.map((line) => (
          <div key={line.id} className={cn(wrap ? 'whitespace-pre-wrap [overflow-wrap:anywhere]' : 'whitespace-pre', line.error && 'text-red')}>
            {line.text || ' '}
          </div>
        ))}
      </div>
    );
  };

  const hasLog = !active && lines.length > 0;

  return (
    <Console>
      <ConsoleBar>
        {stateLabel()}
        {hasLog && (
          <>
            <label className="flex h-7 min-w-0 max-w-[200px] flex-1 items-center gap-1.5 rounded-lg bg-console-fill px-2.5 text-console-label-3 has-[input:focus-visible]:outline has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-accent-fill">
              <Search aria-hidden className="h-[13px] w-[13px] shrink-0" />
              <input
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
                placeholder={t('containers.logsFilter')}
                aria-label={t('containers.logsFilterLabel')}
                className="min-w-0 flex-1 bg-transparent font-sans text-[12px] text-console-label outline-none placeholder:text-console-label-3"
              />
            </label>
            <span className="flex-1" />
            <ConsoleButton icon={WrapText} iconOnly label={t('containers.logsWrap')} pressed={wrap} onClick={() => setWrap(!wrap)} />
            <ConsoleButton icon={Download} iconOnly label={t('images.logDownload')} onClick={download} />
          </>
        )}
      </ConsoleBar>
      {body()}
      {hasLog && !following && shown.length > 0 && (
        <button
          type="button"
          onClick={() => setFollowing(true)}
          className="absolute bottom-4 left-1/2 inline-flex h-8 -translate-x-1/2 items-center gap-1.5 rounded-full bg-console-fill-hover px-3.5 font-sans text-[12px] font-semibold text-console-label shadow-[var(--console-shadow)]"
        >
          <ArrowDown aria-hidden className="h-3.5 w-3.5" />
          {t('containers.logsLatest')}
        </button>
      )}
    </Console>
  );
}
