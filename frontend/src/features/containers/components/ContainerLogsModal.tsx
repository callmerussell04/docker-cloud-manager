import { useEffect, useRef, useState } from 'react';
import { Terminal, Download } from 'lucide-react';
import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { getLogsTicketFn } from '../api';
import { API_URL } from '@/config';
import { useToastStore } from '@/store/toastStore';
import type { ContainerData } from '../types';

interface ContainerLogsModalProps {
  container: ContainerData | null;
  isAdmin?: boolean;
  onClose: () => void;
}

export function ContainerLogsModal({ container, isAdmin = false, onClose }: ContainerLogsModalProps) {
  const [logs, setLogs] = useState<string[]>([]);
  const[tail, setTail] = useState('200');
  const [timestamps, setTimestamps] = useState(true);
  const [follow, setFollow] = useState(true);
  const [isConnected, setIsConnected] = useState(false);
  const scrollRef = useRef<HTMLPreElement>(null);
  const esRef = useRef<EventSource | null>(null);
  const addToast = useToastStore((state) => state.addToast);

  useEffect(() => {
    if (!container) return;
    let isSubscribed = true;

    const connect = async () => {
      try {
        setLogs([]);
        setIsConnected(false);
        const { ticket } = await getLogsTicketFn(container.id, isAdmin);
        if (!isSubscribed) return;

        const prefix = isAdmin ? '/admin' : '';
        const url = new URL(`${API_URL}${prefix}/containers/${container.id}/logs/stream`);
        url.searchParams.set('ticket', ticket);
        url.searchParams.set('tail', tail);
        url.searchParams.set('timestamps', timestamps.toString());
        url.searchParams.set('follow', follow.toString());

        const es = new EventSource(url.toString());
        esRef.current = es;

        es.onopen = () => setIsConnected(true);

        es.addEventListener('log', (e) => {
          setLogs((prev) => [...prev, e.data]);
        });

        es.addEventListener('error', (e: any) => {
          addToast(e.data || 'Ошибка потока логов', 'error');
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
      } catch (e) {
        addToast('Не удалось получить тикет для логов', 'error');
        onClose();
      }
    };

    connect();

    return () => {
      isSubscribed = false;
      if (esRef.current) {
        esRef.current.close();
      }
    };
  }, [container, isAdmin, tail, timestamps, follow]);

  useEffect(() => {
    if (follow && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  },[logs, follow]);

  const downloadLogs = () => {
    const blob = new Blob([logs.join('\n')], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${container?.name || 'container'}.log`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  };

  if (!container) return null;

  return (
    <Modal isOpen={!!container} onClose={onClose} title="Логи контейнера" className="max-w-5xl">
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-4 bg-slate-50 dark:bg-slate-800/50 p-3 rounded-xl border border-slate-200 dark:border-slate-700/50">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium">Tail:</span>
            <Input type="number" value={tail} onChange={(e) => setTail(e.target.value)} className="w-24 h-8" />
          </div>
          <label className="flex items-center gap-2 text-sm cursor-pointer">
            <input type="checkbox" checked={timestamps} onChange={(e) => setTimestamps(e.target.checked)} className="rounded text-indigo-600 focus:ring-indigo-500" />
            Timestamps
          </label>
          <label className="flex items-center gap-2 text-sm cursor-pointer">
            <input type="checkbox" checked={follow} onChange={(e) => setFollow(e.target.checked)} className="rounded text-indigo-600 focus:ring-indigo-500" />
            Follow
          </label>
          <div className="flex-1" />
          <div className="flex items-center gap-2">
            <div className={`w-2 h-2 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`} />
            <span className="text-xs text-slate-500">{isConnected ? 'Подключено' : 'Отключено'}</span>
          </div>
          <Button variant="secondary" onClick={downloadLogs} className="h-8 px-3 text-xs">
            <Download className="w-3 h-3 mr-2" />
            Скачать
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
            {logs.length === 0 && isConnected && 'Ожидание логов...'}
            {logs.join('\n')}
          </pre>
        </div>
      </div>
    </Modal>
  );
}