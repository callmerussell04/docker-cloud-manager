import type { ReactNode } from 'react';

interface EmptyStateProps {
  icon: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
}

export function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center p-12 text-center">
      <div className="mb-4 flex h-16 w-16 items-center justify-center rounded-2xl bg-indigo-100 text-indigo-600 dark:bg-indigo-900/30 dark:text-indigo-400">
        {icon}
      </div>
      <h3 className="mb-2 text-xl font-semibold">{title}</h3>
      {description && <p className="mb-6 max-w-sm text-slate-500 dark:text-slate-400">{description}</p>}
      {action}
    </div>
  );
}
