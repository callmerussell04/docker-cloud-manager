import { useEffect, useMemo, useRef, useState } from 'react';
import { Terminal, Download } from 'lucide-react';
import { Modal } from '@/shared/ui/Modal';
import { Button } from '@/shared/ui/Button';
import { Input } from '@/shared/ui/Input';
import { containerStreamPath, getLogsTicketFn } from '../api';
import { API_URL } from '@/shared/lib/config';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import type { ContainerData } from '../model';
import type { Scope } from '@/shared/api/scope';
import { failureReason, getServerErrorMessage } from '@/shared/api/apiError';
import type { TFunction } from '@/shared/i18n';
import { useT, useTRef } from '@/shared/i18n';
import { ConnectionStatus } from '@/shared/ui/ConnectionStatus';

// The most lines the modal keeps and the largest tail it asks for.
const maxLogLines = 5000;

interface ContainerLogsModalProps {
  container: ContainerData | null;
  scope: Scope;
  onClose: () => void;
}

export function ContainerLogsModal({ container, scope, onClose }: ContainerLogsModalProps) {
  const [logs, setLogs] = useState<string[]>([]);
  const[tail, setTail] = useState('200');
  const [appliedTail, setAppliedTail] = useState('200');
  const [timestamps, setTimestamps] = useState(true);
  const [appliedTimestamps, setAppliedTimestamps] = useState(true);
  const [follow, setFollow] = useState(true);
  const [appliedFollow, setAppliedFollow] = useState(true);
  const [isConnected, setIsConnected] = useState(false);
  const scrollRef = useRef<HTMLPreElement>(null);
  const esRef = useRef<EventSource | null>(null);
  const notify = useNotificationStore((state) => state.notify);
  const t = useT();
  const tRef = useTRef();

  useEffect(() => {
    if (!container) return;
    let isSubscribed = true;
    let pending: string[] = [];
    let flushFrame = 0;

    const connect = async () => {
      try {
        setLogs([]);
        setIsConnected(false);
        esRef.current?.close();
        const { ticket } = await getLogsTicketFn(scope, container.id);
        if (!isSubscribed) return;

        const url = new URL(`${API_URL}${containerStreamPath(scope, container.id, 'logs')}`);
        url.searchParams.set('ticket', ticket);
        url.searchParams.set('tail', appliedTail);
        url.searchParams.set('timestamps', appliedTimestamps.toString());
        url.searchParams.set('follow', appliedFollow.toString());

        const es = new EventSource(url.toString());
        esRef.current = es;

        es.onopen = () => setIsConnected(true);

        // Lines arrive one event each; a burst, such as the tail a stream
        // opens with, is added once per frame instead of once per line.
        es.addEventListener('log', (e) => {
          pending.push(e.data);
          if (flushFrame) return;
          flushFrame = requestAnimationFrame(() => {
            flushFrame = 0;
            const lines = pending;
            pending = [];
            setLogs((prev) => prev.concat(lines).slice(-maxLogLines));
          });
        });

        es.addEventListener('error', (event) => {
          const title = tRef.current('containers.logsStreamError');
          const reason = getStreamErrorMessage('data' in event ? event.data : undefined, title, tRef.current);
          notify(title, 'error', reason === title ? undefined : reason);
          es.close();
          setIsConnected(false);
        });

        es.addEventListener('end', () => {
          es.close();
          setIsConnected(false);
        });

        es.onerror = () => {
          es.close();
          setIsConnected(false);
        };
      } catch (error) {
        notify(tRef.current('containers.logsTicketFailed'), 'error', failureReason(error, tRef.current));
        onClose();
      }
    };

    connect();

    return () => {
      isSubscribed = false;
      cancelAnimationFrame(flushFrame);
      if (esRef.current) {
        esRef.current.close();
      }
    };
  }, [container, scope, appliedTail, appliedTimestamps, appliedFollow, notify, onClose, tRef]);

  useEffect(() => {
    if (follow && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  },[logs, follow]);

  const logText = useMemo(() => logs.join('\n'), [logs]);

  const downloadLogs = () => {
    const blob = new Blob([logText], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${container?.name || 'container'}.log`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  const applyOptions = () => {
    const nextTail = Math.max(0, Math.min(maxLogLines, Number(tail) || 0)).toString();
    setTail(nextTail);
    setAppliedTail(nextTail);
    setAppliedTimestamps(timestamps);
    setAppliedFollow(follow);
  };

  if (!container) return null;

  return (
    <Modal isOpen={!!container} onClose={onClose} title={t('containers.logsTitle')} className="max-w-5xl">
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-4 bg-slate-50 dark:bg-slate-800/50 p-3 rounded-xl border border-slate-200 dark:border-slate-700/50">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium">{t('containers.logsTail')}</span>
            <Input type="number" value={tail} onChange={(e) => setTail(e.target.value)} className="w-24 h-8" />
          </div>
          <label className="flex items-center gap-2 text-sm cursor-pointer">
            <input type="checkbox" checked={timestamps} onChange={(e) => setTimestamps(e.target.checked)} className="rounded accent-accent-fill" />
            {t('containers.logsTimestamps')}
          </label>
          <label className="flex items-center gap-2 text-sm cursor-pointer">
            <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} className="rounded accent-accent-fill" />
            {t('containers.logsFollow')}
          </label>
          <Button
            type="button"
            variant="secondary"
            onClick={applyOptions}
            className="h-8 px-3 text-xs"
          >
            {t('common.apply')}
          </Button>
          <div className="flex-1" />
          <ConnectionStatus connected={isConnected} connectedLabel={t('connection.connected')} disconnectedLabel={t('connection.disconnected')} />
          <Button variant="secondary" onClick={downloadLogs} className="h-8 px-3 text-xs">
            <Download className="w-3 h-3 mr-2" />
            {t('common.download')}
          </Button>
        </div>
        <div className="bg-slate-950 rounded-xl border border-slate-800 overflow-hidden relative group h-[60vh]">
          <div className="absolute top-0 left-0 right-0 h-8 bg-slate-900 border-b border-slate-800 flex items-center px-4 z-10">
            <div className="flex items-center gap-2 text-slate-400">
              <Terminal className="w-3.5 h-3.5" />
              <span className="text-xs font-mono">{container.name}</span>
            </div>
          </div>
          <pre ref={scrollRef} className="p-4 pt-12 h-full w-full overflow-auto text-[13px] font-mono text-slate-300 whitespace-pre-wrap break-words">
            {logs.length === 0 && isConnected && t('containers.logsWaiting')}
            {logText}
          </pre>
        </div>
      </div>
    </Modal>
  );
}

// getStreamErrorMessage shows the stream's error only when it is safe for the
// user to see, and the fallback otherwise.
function getStreamErrorMessage(data: unknown, fallback: string, t: TFunction) {
  if (typeof data !== 'string' || !data) {
    return fallback;
  }

  let message = data;
  try {
    message = (JSON.parse(data) as { error?: string }).error || data;
  } catch {
    // A plain-text event carries the message itself.
  }
  return getServerErrorMessage(message, t) ?? fallback;
}
