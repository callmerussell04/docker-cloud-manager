import { useEffect, useRef, useState } from 'react';
import { RotateCw } from 'lucide-react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import type { Scope } from '@/shared/api/scope';
import { failureReason, getServerErrorMessage } from '@/shared/api/apiError';
import { WS_URL } from '@/shared/lib/config';
import { useT, useTRef } from '@/shared/i18n';
import { Console, ConsoleBar, ConsoleButton, ConsoleState } from '@/shared/ui/Console';
import { useNotificationStore } from '@/shared/ui/notificationStore';
import { useShellLayout } from '@/shared/ui/shellLayout';
import { containerStreamPath, getTerminalTicketFn } from '../api';

type SessionState = 'connecting' | 'open' | 'closed';

// cssValue reads a design token, as xterm takes literal colours and fonts.
function cssValue(name: string) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

interface ContainerTerminalProps {
  scope: Scope;
  containerId: string;
}

// ContainerTerminal is the container page's terminal: xterm over a WebSocket
// opened with a one-time ticket, so no token is in its URL. The session lasts
// while it is shown; switching to the logs or leaving the page closes it.
// Mount it only for a running container.
export function ContainerTerminal({ scope, containerId }: ContainerTerminalProps) {
  const t = useT();
  const tRef = useTRef();
  const compact = useShellLayout().rowsCompact;
  const notify = useNotificationStore((store) => store.notify);
  const hostRef = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<SessionState>('connecting');
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    const term = new Terminal({
      cursorBlink: true,
      theme: { background: cssValue('--console'), foreground: cssValue('--console-label') },
      fontFamily: cssValue('--font-mono') || 'monospace',
      fontSize: 13,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();

    let subscribed = true;
    let socket: WebSocket | null = null;
    const disposables: { dispose: () => void }[] = [];

    const connect = async () => {
      setState('connecting');
      try {
        const { ticket } = await getTerminalTicketFn(scope, containerId);
        if (!subscribed) return;

        const url = new URL(`${WS_URL}${containerStreamPath(scope, containerId, 'terminal')}`);
        url.searchParams.set('ticket', ticket);
        url.searchParams.set('cols', String(term.cols));
        url.searchParams.set('rows', String(term.rows));

        const ws = new WebSocket(url.toString());
        socket = ws;
        ws.binaryType = 'arraybuffer';
        ws.onopen = () => setState('open');

        ws.onmessage = (event) => {
          if (!subscribed) return;
          if (typeof event.data !== 'string') {
            term.write(new Uint8Array(event.data));
            return;
          }
          try {
            const message = JSON.parse(event.data);
            if (message.type === 'ready') {
              term.focus();
            } else if (message.type === 'error') {
              const error = getServerErrorMessage(message.error, tRef.current) ?? tRef.current('containers.terminalError');
              term.writeln(`\r\n\x1b[31m${error}\x1b[0m\r\n`);
            } else if (message.type === 'exit') {
              term.writeln(`\r\n\x1b[33m${tRef.current('containers.terminalExited', { code: message.exit_code })}\x1b[0m\r\n`);
            }
          } catch {
            term.write(event.data);
          }
        };

        ws.onclose = () => {
          // The terminal is disposed once the console has gone.
          if (!subscribed) return;
          setState('closed');
          term.writeln(`\r\n\x1b[33m${tRef.current('containers.terminalDisconnected')}\x1b[0m\r\n`);
        };

        disposables.push(
          term.onData((data) => {
            if (ws.readyState === WebSocket.OPEN) ws.send(new TextEncoder().encode(data));
          }),
          term.onResize((size) => {
            if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'resize', cols: size.cols, rows: size.rows }));
          }),
        );
      } catch (error) {
        if (!subscribed) return;
        notify(tRef.current('containers.terminalTicketFailed'), 'error', failureReason(error, tRef.current));
        setState('closed');
      }
    };

    void connect();

    const resizeObserver = new ResizeObserver(() => fit.fit());
    resizeObserver.observe(host);

    return () => {
      subscribed = false;
      resizeObserver.disconnect();
      disposables.forEach((disposable) => disposable.dispose());
      socket?.close();
      term.dispose();
    };
  }, [scope, containerId, attempt, notify, tRef]);

  return (
    <Console>
      <ConsoleBar>
        {state === 'open' ? (
          <ConsoleState live>{t('containers.terminalConnected')}</ConsoleState>
        ) : (
          <ConsoleState live={false}>{state === 'connecting' ? t('containers.consoleConnecting') : t('containers.consoleDisconnected')}</ConsoleState>
        )}
        {state === 'closed' && <ConsoleButton icon={RotateCw} label={t('containers.consoleReconnect')} onClick={() => setAttempt((current) => current + 1)} />}
        <div className="flex-1" />
        {!compact && <span className="truncate text-[12px] text-console-label-3">{t('containers.terminalHint')}</span>}
      </ConsoleBar>
      {/* xterm stops its own key events, so Ctrl K still reaches the shell. */}
      <div className="min-h-0 flex-1 px-3 pb-2 pt-2">
        <div ref={hostRef} className="h-full w-full" />
      </div>
    </Console>
  );
}
