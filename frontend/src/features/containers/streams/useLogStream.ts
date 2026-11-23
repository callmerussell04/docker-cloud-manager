import { useCallback, useEffect, useState } from 'react';
import type { Scope } from '@/shared/api/scope';
import { failureReason, getServerErrorMessage } from '@/shared/api/apiError';
import { API_URL } from '@/shared/lib/config';
import { useTRef, type TFunction } from '@/shared/i18n';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { containerStreamPath, getLogsTicketFn } from '../api';
import { splitLogTimestamp, type LogLine } from './logLines';

// The tail a stream opens with, and the most lines the console keeps.
const tailLines = 1000;
const maxLogLines = 5000;

// LogStreamState is where a log stream is: asking for its ticket and opening,
// open, finished by the server (a stopped container's last output has all
// arrived), or lost (refused, failed or dropped while following).
export type LogStreamState = 'connecting' | 'open' | 'ended' | 'lost';

// useLogStream streams a container's log while the component that uses it is
// mounted. It takes a one-time ticket first, so no token is in the stream's
// URL, and closes the stream when it unmounts or its inputs change. follow
// keeps the stream open for new lines, as for a running container; without it
// the stream sends the last output and ends.
export function useLogStream(scope: Scope, containerId: string, follow: boolean) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const [state, setState] = useState<LogStreamState>('connecting');
  const [attempt, setAttempt] = useState(0);
  const notify = useNotificationStore((store) => store.notify);
  const tRef = useTRef();

  useEffect(() => {
    let subscribed = true;
    let source: EventSource | null = null;
    let pending: LogLine[] = [];
    let flushFrame = 0;
    let nextId = 0;

    const connect = async () => {
      setLines([]);
      setState('connecting');
      try {
        const { ticket } = await getLogsTicketFn(scope, containerId);
        if (!subscribed) return;

        const url = new URL(`${API_URL}${containerStreamPath(scope, containerId, 'logs')}`);
        url.searchParams.set('ticket', ticket);
        url.searchParams.set('tail', String(tailLines));
        url.searchParams.set('timestamps', 'true');
        url.searchParams.set('follow', String(follow));

        const es = new EventSource(url.toString());
        source = es;
        es.onopen = () => setState('open');

        // Lines arrive one event each; a burst, such as the tail a stream
        // opens with, is added once per frame instead of once per line.
        es.addEventListener('log', (event) => {
          pending.push({ id: nextId++, ...splitLogTimestamp(event.data) });
          if (flushFrame) return;
          flushFrame = requestAnimationFrame(() => {
            flushFrame = 0;
            const arrived = pending;
            pending = [];
            setLines((current) => current.concat(arrived).slice(-maxLogLines));
          });
        });

        es.addEventListener('error', (event) => {
          const title = tRef.current('containers.logsStreamError');
          const reason = streamErrorMessage('data' in event ? event.data : undefined, title, tRef.current);
          notify(title, 'error', reason === title ? undefined : reason);
          es.close();
          setState('lost');
        });

        es.addEventListener('end', () => {
          es.close();
          setState('ended');
        });

        // The connection dropped. A stream that was not following has sent
        // what it had; one that was is lost and can be reconnected.
        es.onerror = () => {
          es.close();
          setState(follow ? 'lost' : 'ended');
        };
      } catch (error) {
        if (!subscribed) return;
        notify(tRef.current('containers.logsTicketFailed'), 'error', failureReason(error, tRef.current));
        setState('lost');
      }
    };

    void connect();

    return () => {
      subscribed = false;
      cancelAnimationFrame(flushFrame);
      source?.close();
    };
  }, [scope, containerId, follow, attempt, notify, tRef]);

  const reconnect = useCallback(() => setAttempt((current) => current + 1), []);
  return { lines, state, reconnect };
}

// streamErrorMessage shows the stream's error only when it is safe for the
// user to see, and the fallback otherwise.
function streamErrorMessage(data: unknown, fallback: string, t: TFunction) {
  if (typeof data !== 'string' || !data) return fallback;
  let message = data;
  try {
    message = (JSON.parse(data) as { error?: string }).error || data;
  } catch {
    // A plain-text event carries the message itself.
  }
  return getServerErrorMessage(message, t) ?? fallback;
}
