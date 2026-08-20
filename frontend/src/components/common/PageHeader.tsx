import type { ReactNode } from 'react';

interface PageHeaderProps {
  title: string;
  subtitle?: string;
  tone?: 'default' | 'danger';
  icon?: ReactNode;
  actions?: ReactNode;
}

export function PageHeader({ title, subtitle, tone = 'default', icon, actions }: PageHeaderProps) {
  return (
    <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 className={`flex items-center gap-3 text-3xl font-bold tracking-tight ${tone === 'danger' ? 'text-red-600 dark:text-red-400' : ''}`}>
          {icon}
          {title}
        </h1>
        {subtitle && <p className="mt-1 text-slate-500 dark:text-slate-400">{subtitle}</p>}
      </div>
      {actions && <div className="flex w-full items-center gap-3 sm:w-auto">{actions}</div>}
    </div>
  );
}
