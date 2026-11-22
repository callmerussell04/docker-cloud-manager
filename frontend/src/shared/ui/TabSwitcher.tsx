import type { ElementType } from 'react';
import { cn } from '@/shared/lib/utils';

interface TabItem<T extends string> {
  id: T;
  label: string;
  icon?: ElementType;
}

interface TabSwitcherProps<T extends string> {
  items: TabItem<T>[];
  activeTab: T;
  onChange: (tab: T) => void;
}

export function TabSwitcher<T extends string>({ items, activeTab, onChange }: TabSwitcherProps<T>) {
  return (
    <div className="max-w-full overflow-x-auto pb-1">
      <div className="flex w-max rounded-xl bg-fill p-1">
        {items.map((item) => {
          const Icon = item.icon;
          return (
            <button
              key={item.id}
              type="button"
              onClick={() => onChange(item.id)}
              className={cn(
                'flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium transition-all',
                activeTab === item.id ? 'bg-glass-dense text-label shadow-sm' : 'text-label-2 hover:text-label'
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
