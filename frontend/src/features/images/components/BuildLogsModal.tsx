import { RefreshCcw, Terminal } from 'lucide-react';
import { useEffect, useRef } from 'react';

import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { type BuildData } from '../types';
import { getApiErrorMessage } from '@/lib/apiError';
import { useT } from '@/lib/i18n';
import { useBuildLogs } from '../hooks';

interface BuildLogsModalProps {
  build: BuildData | null;
  isAdmin?: boolean;
  onClose: () => void;
}

export function BuildLogsModal({ build, isAdmin = false, onClose }: BuildLogsModalProps) {
  const scrollRef = useRef<HTMLPreElement>(null);
  const t = useT();

  const { data: logs, error, isLoading, isError, refetch, isFetching } = useBuildLogs(build?.id, isAdmin, !!build, build?.status === 'running');

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs]);

  if (!build) return null;
  const logsError = isError ? getApiErrorMessage(error, t('images.loadLogsFailed'), t) : null;

  return (
    <Modal isOpen={!!build} onClose={onClose} title={t('images.buildLogsTitle')} className="max-w-4xl">
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
          {isLoading && !logs && t('images.logsLoading')}
          {logsError && !logs && logsError.message}
          {logs && !logs.trim() && t('images.logsEmpty')}
          {logs}
        </pre>
      </div>

      <div className="flex justify-end gap-3 pt-6 mt-2">
        <Button onClick={onClose}>{t('common.close')}</Button>
      </div>
    </Modal>
  );
}
