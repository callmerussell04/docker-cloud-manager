import type { ElementType } from 'react';
import { cn } from '@/lib/utils';

interface TabItem<T extends string> {
  id: T;
  label: string;
  icon?: ElementType;
}

interface TabSwitcherProps<T extends string> {
  items: TabItem<T>[];
  activeTab: T;
  onChange: (tab: T) => void;
  tone?: 'default' | 'danger';
}

export function TabSwitcher<T extends string>({ items, activeTab, onChange, tone = 'default' }: TabSwitcherProps<T>) {
  const activeClass = tone === 'danger'
    ? 'bg-white text-red-600 shadow-sm dark:bg-slate-800 dark:text-red-400'
    : 'bg-white text-indigo-600 shadow-sm dark:bg-slate-800 dark:text-indigo-400';

  return (
    <div className="max-w-full overflow-x-auto pb-1">
      <div className="flex w-max rounded-xl border border-white/50 bg-white/40 p-1 backdrop-blur-md dark:border-slate-700/50 dark:bg-slate-900/40">
        {items.map((item) => {
          const Icon = item.icon;
          return (
            <button
              key={item.id}
              type="button"
              onClick={() => onChange(item.id)}
              className={cn(
                'flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                activeTab === item.id ? activeClass : 'text-slate-600 hover:text-slate-900 dark:text-slate-400 dark:hover:text-slate-100'
              )}
            >
              {Icon && <Icon className="h-4 w-4" />}
              {item.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}
