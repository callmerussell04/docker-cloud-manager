import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import { ArrowDown, Download, RotateCw, Search, WrapText } from 'lucide-react';
import type { Scope } from '@/shared/api/scope';
import { dateLocale, useLocale, useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { Console, ConsoleBar, ConsoleButton, ConsoleState } from '@/shared/ui/Console';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { filterLogLines, isAtLatest, type LogLine } from './logLines';
import { useLogStream } from './useLogStream';

interface ContainerLogsProps {
  scope: Scope;
  containerId: string;
  // name names the downloaded file.
  name: string;
  // running follows the log live; a stopped container's log is its last
  // output.
  running: boolean;
}

// ContainerLogs is the container page's log console. It follows the newest
// line until the reader scrolls up, then offers a Latest button back; the
// filter narrows the lines shown, and timestamps, wrapping and a download
// sit beside it. Its stream is open only while it is shown.
export function ContainerLogs({ scope, containerId, name, running }: ContainerLogsProps) {
  const t = useT();
  const locale = useLocale();
  const compact = useShellLayout().rowsCompact;
  const { lines, state, reconnect } = useLogStream(scope, containerId, running);
  const [filter, setFilter] = useState('');
  const [timestamps, setTimestamps] = useState(true);
  const [wrap, setWrap] = useState(false);
  const [following, setFollowing] = useState(true);
  const bodyRef = useRef<HTMLDivElement>(null);

  const shown = useMemo(() => filterLogLines(lines, filter), [lines, filter]);
  const showTimes = timestamps && !compact;
  const timeFormat = useMemo(
    () => new Intl.DateTimeFormat(dateLocale(locale), { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false }),
    [locale],
  );

  useLayoutEffect(() => {
    const body = bodyRef.current;
    if (following && body) body.scrollTop = body.scrollHeight;
  }, [shown, following, wrap, showTimes]);

  const download = () => {
    const text = lines.map((line) => (line.timestamp ? `${line.timestamp} ${line.text}` : line.text)).join('\n');
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
    const link = document.createElement('a');
    link.href = url;
    link.download = `${name}.log`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  };

  const stateLabel = () => {
    if (state === 'lost') return <ConsoleState live={false}>{t('containers.consoleDisconnected')}</ConsoleState>;
    if (!running) return <ConsoleState live={false}>{t('containers.logsLastOutput')}</ConsoleState>;
    if (state === 'connecting') return <ConsoleState live={false}>{t('containers.consoleConnecting')}</ConsoleState>;
    return <ConsoleState live>{t('containers.logsLive')}</ConsoleState>;
  };

  const empty = () => {
    if (state === 'connecting') return null;
    if (lines.length === 0) return running && state === 'open' ? t('containers.logsWaiting') : t('containers.logsEmpty');
    return t('containers.logsNoMatch');
  };

  return (
    <Console>
      <ConsoleBar>
        {stateLabel()}
        {state === 'lost' && <ConsoleButton icon={RotateCw} label={t('containers.consoleReconnect')} onClick={reconnect} />}
        <label className="flex h-7 min-w-0 max-w-[200px] flex-1 items-center gap-1.5 rounded-lg bg-console-fill px-2.5 text-console-label-3 has-[input:focus-visible]:outline has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-accent-fill">
          <Search aria-hidden className="h-[13px] w-[13px] shrink-0" />
          <input
            type="search"
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            placeholder={t('containers.logsFilter')}
            aria-label={t('containers.logsFilterLabel')}
            onKeyDown={(event) => {
              if (event.key === 'Escape' && filter) {
                event.preventDefault();
                setFilter('');
              }
            }}
            className="w-full min-w-0 bg-transparent font-sans text-[12.5px] text-console-label placeholder:text-console-label-3 focus-visible:outline-none [&::-webkit-search-cancel-button]:hidden"
          />
        </label>
        <div className="flex-1" />
        {!compact && <ConsoleButton label={t('containers.logsTimestamps')} pressed={timestamps} onClick={() => setTimestamps(!timestamps)} />}
        <ConsoleButton icon={WrapText} iconOnly label={t('containers.logsWrap')} pressed={wrap} onClick={() => setWrap(!wrap)} />
        <ConsoleButton icon={Download} iconOnly label={t('containers.logsDownload')} disabled={lines.length === 0} onClick={download} />
      </ConsoleBar>
      <div
        ref={bodyRef}
        tabIndex={0}
        role="log"
        aria-label={t('containers.logsOf', { name })}
        onScroll={(event) => setFollowing(isAtLatest(event.currentTarget))}
        className="min-h-0 flex-1 overflow-auto px-4 pb-3.5 pt-2.5 [scrollbar-color:var(--console-fill-hover)_transparent] focus-visible:outline-none"
      >
        {shown.length === 0 ? (
          <div className="py-2 font-sans text-console-label-3">{empty()}</div>
        ) : (
          shown.map((line) => <LogRow key={line.id} line={line} wrap={wrap} time={showTimes ? formatTime(line, timeFormat) : undefined} />)
        )}
      </div>
      {!following && (
        <button
          type="button"
          onClick={() => setFollowing(true)}
          className="absolute bottom-4 right-[18px] inline-flex h-[30px] items-center gap-1.5 rounded-full bg-console-fill-hover px-3 font-sans text-[12.5px] font-semibold text-console-label hover:brightness-125"
        >
          <ArrowDown aria-hidden className="h-3.5 w-3.5" />
          {t('containers.logsLatest')}
        </button>
      )}
    </Console>
  );
}

function LogRow({ line, wrap, time }: { line: LogLine; wrap: boolean; time?: string }) {
  return (
    <div className={cn('flex gap-3.5 leading-[1.72]', wrap ? 'whitespace-pre-wrap break-all' : 'whitespace-pre')}>
      {time !== undefined && <span className="flex-none text-console-label-3">{time}</span>}
      <span>{line.text}</span>
    </div>
  );
}

// formatTime shows a line's timestamp as a local time of day. Docker gives
// nanoseconds, which Date does not read, so the fraction is cut to
// milliseconds first.
function formatTime(line: LogLine, format: Intl.DateTimeFormat) {
  if (!line.timestamp) return '';
  const date = new Date(line.timestamp.replace(/(\.\d{3})\d+/, '$1'));
  return Number.isNaN(date.getTime()) ? line.timestamp : format.format(date);
}
