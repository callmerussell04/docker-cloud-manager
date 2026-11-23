import { Link } from 'react-router-dom';
import { ChevronRight } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { Dot, type StatusTone } from '@/shared/ui/StatusDot';
import { cn } from '@/shared/lib/utils';

// AttentionItem is a container that needs the user: its name, the line under
// it (its status, or why it failed), and the page it opens.
export interface AttentionItem {
  id: string;
  name: string;
  tone: StatusTone;
  detail: string;
  to: string;
}

function AttentionRow({ item }: { item: AttentionItem }) {
  return (
    <Link
      to={item.to}
      className="list-row relative flex min-h-[48px] items-center gap-3 rounded-[12px] py-1.5 pl-1.5 pr-2 transition-colors hover:bg-fill"
    >
      <Dot tone={item.tone} className="ml-1" />
      <span className="grid min-w-0 flex-1">
        <span className="truncate font-semibold text-label">{item.name}</span>
        <span className={cn('truncate text-xs', item.tone === 'bad' ? 'text-red-text' : 'text-label-2')} title={item.detail}>
          {item.detail}
        </span>
      </span>
      <ChevronRight aria-hidden className="h-4 w-4 shrink-0 text-label-3" />
    </Link>
  );
}

function Section({ title, items, tone }: { title: string; items: readonly AttentionItem[]; tone?: 'bad' }) {
  return (
    <section className="grid gap-1">
      <h2 className={cn('px-1.5 text-xs font-semibold', tone === 'bad' ? 'text-red-text' : 'text-label-2')}>{title}</h2>
      <div>
        {items.map((item) => (
          <AttentionRow key={item.id} item={item} />
        ))}
      </div>
    </section>
  );
}

// AttentionColumn lists what needs the user beside the memory map: failing
// containers, then those starting or stopping. With neither it says so in one
// quiet line.
export function AttentionColumn({ failing, changing }: { failing: readonly AttentionItem[]; changing: readonly AttentionItem[] }) {
  const t = useT();
  if (failing.length === 0 && changing.length === 0) {
    return (
      <div className="grid gap-0.5 px-1.5">
        <p className="font-semibold text-label">{t('dashboard.attention.none')}</p>
        <p className="text-label-2">{t('dashboard.attention.noneHint')}</p>
      </div>
    );
  }
  return (
    <div className="grid gap-5">
      {failing.length > 0 && <Section title={t('dashboard.attention.failing')} items={failing} tone="bad" />}
      {changing.length > 0 && <Section title={t('dashboard.attention.changing')} items={changing} />}
    </div>
  );
}
