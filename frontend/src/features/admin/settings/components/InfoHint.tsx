import { useState } from 'react';
import { Info } from 'lucide-react';
import type { TFunction } from '@/shared/i18n';

export function InfoHint({ text, label, t }: { text: string; label: string; t: TFunction }) {
  const [open, setOpen] = useState(false);

  return (
    <span className="relative inline-flex">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        onBlur={() => window.setTimeout(() => setOpen(false), 120)}
        className="inline-flex h-5 w-5 items-center justify-center rounded-full border border-slate-300 bg-white/70 text-slate-500 transition-colors hover:text-indigo-600 focus:outline-none focus:ring-2 focus:ring-indigo-500 dark:border-slate-600 dark:bg-slate-900/70 dark:text-slate-400 dark:hover:text-indigo-300"
        aria-label={t('admin.settings.showHint', { label })}
        aria-expanded={open}
      >
        <Info className="h-3.5 w-3.5" />
      </button>
      {open && (
        <span className="absolute left-1/2 top-7 z-20 w-72 -translate-x-1/2 rounded-lg border border-slate-200 bg-white p-3 text-xs leading-5 text-slate-600 shadow-xl dark:border-slate-700 dark:bg-slate-950 dark:text-slate-300">
          {text}
        </span>
      )}
    </span>
  );
}
