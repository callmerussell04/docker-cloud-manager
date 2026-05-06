import { useEffect, useRef, useState } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { Modal } from '@/components/ui/Modal';
import { getTerminalTicketFn } from '../api';
import { WS_URL } from '@/config';
import { useToastStore } from '@/store/toastStore';
import type { ContainerData } from '../types';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';

interface ContainerTerminalModalProps {
  container: ContainerData | null;
  isAdmin?: boolean;
  onClose: () => void;
}

export function ContainerTerminalModal({ container, isAdmin = false, onClose }: ContainerTerminalModalProps) {
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const addToast = useToastStore((state) => state.addToast);
  const t = useT();

  useEffect(() => {
    if (!container || !terminalRef.current) return;

    const term = new Terminal({
      cursorBlink: true,
      theme: { background: '#020617', foreground: '#cbd5e1' },
      fontFamily: 'monospace',
      fontSize: 14,
    });
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(terminalRef.current);
    fitAddon.fit();

    xtermRef.current = term;
    fitAddonRef.current = fitAddon;

    let isSubscribed = true;

    const connect = async () => {
      try {
        const { ticket } = await getTerminalTicketFn(container.id, isAdmin);
        if (!isSubscribed) return;

        const prefix = isAdmin ? '/admin' : '';
        const url = new URL(`${WS_URL}${prefix}/containers/${container.id}/terminal`);
        url.searchParams.set('ticket', ticket);
        url.searchParams.set('cols', term.cols.toString());
        url.searchParams.set('rows', term.rows.toString());

        const ws = new WebSocket(url.toString());
        wsRef.current = ws;

        ws.binaryType = 'arraybuffer';

        ws.onopen = () => {
          setIsConnected(true);
        };

        ws.onmessage = (event) => {
          if (typeof event.data === 'string') {
            try {
              const msg = JSON.parse(event.data);
              if (msg.type === 'ready') {
                term.focus();
              } else if (msg.type === 'error') {
                term.writeln(`\r\n\x1b[31mError: ${msg.error}\x1b[0m\r\n`);
              } else if (msg.type === 'exit') {
                term.writeln(`\r\n\x1b[33mProcess exited with code ${msg.exit_code}\x1b[0m\r\n`);
              }
            } catch {
              term.write(event.data);
            }
          } else {
            term.write(new Uint8Array(event.data));
          }
        };

        ws.onclose = () => {
          setIsConnected(false);
          term.writeln('\r\n\x1b[33mDisconnected from terminal.\x1b[0m\r\n');
        };

        term.onData((data) => {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(new TextEncoder().encode(data));
          }
        });

        term.onResize((size) => {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: 'resize', cols: size.cols, rows: size.rows }));
          }
        });
      } catch (error) {
        const { message, requestId } = getApiErrorMessage(error, t('containers.terminalTicketFailed'), t);
        addToast(message, 'error', { requestId });
        onClose();
      }
    };

    connect();

    const resizeObserver = new ResizeObserver(() => {
      if (fitAddonRef.current && xtermRef.current) {
        fitAddonRef.current.fit();
      }
    });
    resizeObserver.observe(terminalRef.current);

    return () => {
      isSubscribed = false;
      resizeObserver.disconnect();
      if (wsRef.current) {
        wsRef.current.close();
      }
      term.dispose();
    };
  }, [container, isAdmin, addToast, onClose]);

  if (!container) return null;

  return (
    <Modal isOpen={!!container} onClose={onClose} title={t('containers.terminalTitle', { name: container.name })} className="max-w-5xl">
      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between px-2">
          <div className="flex items-center gap-2">
            <div className={`w-2 h-2 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`} />
            <span className="text-xs text-slate-500">{isConnected ? t('connection.connected') : t('connection.disconnected')}</span>
          </div>
          <span className="text-xs text-slate-500">
            {t('containers.terminalExitHint')}
          </span>
        </div>
        <div className="bg-[#020617] rounded-xl border border-slate-800 p-2 overflow-hidden h-[60vh]">
          <div ref={terminalRef} className="w-full h-full" />
        </div>
      </div>
    </Modal>
  );
}
