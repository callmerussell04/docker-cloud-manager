import { GitBranch, UploadCloud } from 'lucide-react';
import { cn } from '@/lib/utils';

export type SourceMode = 'archive' | 'git';

interface SourceModeSwitchProps {
  value: SourceMode;
  onChange: (mode: SourceMode) => void;
  archiveLabel: string;
  gitLabel: string;
  gitDisabled?: boolean;
}

export function SourceModeSwitch({ value, onChange, archiveLabel, gitLabel, gitDisabled }: SourceModeSwitchProps) {
  const buttonClass = (active: boolean) => cn(
    'inline-flex items-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50',
    active
      ? 'bg-white text-slate-900 shadow-sm dark:bg-slate-800 dark:text-slate-100'
      : 'text-slate-500 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100'
  );

  return (
    <div className="inline-flex rounded-xl border border-slate-200 bg-slate-100/70 p-1 dark:border-slate-700/50 dark:bg-slate-900/60">
      <button type="button" onClick={() => onChange('archive')} className={buttonClass(value === 'archive')}>
        <UploadCloud className="h-4 w-4" />
        {archiveLabel}
      </button>
      <button type="button" onClick={() => onChange('git')} disabled={gitDisabled} className={buttonClass(value === 'git')}>
        <GitBranch className="h-4 w-4" />
        {gitLabel}
      </button>
    </div>
  );
}
