import { useQuery } from '@tanstack/react-query';
import { RefreshCcw, Terminal } from 'lucide-react';
import { useEffect, useRef } from 'react';

import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { getBuildLogsFn } from '../api';
import { type BuildData } from '../types';

interface BuildLogsModalProps {
  build: BuildData | null;
  onClose: () => void;
}

export function BuildLogsModal({ build, onClose }: BuildLogsModalProps) {
  const scrollRef = useRef<HTMLPreElement>(null);

  const { data: logs, isLoading, isError, refetch, isFetching } = useQuery({
    queryKey: ['buildLogs', build?.id],
    queryFn: () => getBuildLogsFn(build!.id),
    enabled: !!build,
    refetchInterval: build?.status === 'running' ? 3000 : false,
    retry: false,
  });

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs]);

  if (!build) return null;

  return (
    <Modal isOpen={!!build} onClose={onClose} title="Логи сборки" className="max-w-4xl max-h-[90vh] flex flex-col">
      <div className="flex-1 min-h-[400px] max-h-[60vh] bg-slate-950 rounded-xl border border-slate-800 overflow-hidden relative group">
        <div className="absolute top-0 left-0 right-0 h-10 bg-slate-900 border-b border-slate-800 flex items-center px-4 justify-between z-10">
          <div className="flex items-center gap-2 text-slate-400">
            <Terminal className="w-4 h-4" />
            <span className="text-xs font-mono">{build.id}.log</span>
          </div>
          <Button variant="ghost" className="h-7 w-7 p-0 text-slate-400 hover:text-white" onClick={() => refetch()}>
            <RefreshCcw className={`w-3.5 h-3.5 ${isFetching ? 'animate-spin' : ''}`} />
          </Button>
        </div>

        <pre 
          ref={scrollRef}
          className="p-4 pt-14 h-full w-full overflow-auto text-xs font-mono text-slate-300 leading-relaxed whitespace-pre-wrap break-all"
        >
          {isLoading && !logs && 'Загрузка логов...'}
          {isError && !logs && 'Ошибка загрузки логов. Возможно, сборка еще не началась или логи удалены.'}
          {logs && !logs.trim() && 'Лог пуст'}
          {logs}
        </pre>
      </div>

      <div className="flex justify-end gap-3 pt-6 mt-2">
        <Button onClick={onClose}>Закрыть</Button>
      </div>
    </Modal>
  );
}