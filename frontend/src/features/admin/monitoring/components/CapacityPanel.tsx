import { useT } from '@/shared/i18n';
import { Meter, type MeterPart } from '@/shared/ui/Meter';
import type { Capacity, CapacityPartKey } from '../model';

// partFill colours each part by what it is: the system's reserve and other
// use gray, containers and DCM data the accent, builds a lighter accent, and
// lent memory orange, as borrowed memory is on the container page.
const partFill: Record<CapacityPartKey, string> = {
  system: 'bg-label-3',
  containers: 'bg-accent-fill',
  builds: 'bg-accent-soft',
  lent: 'bg-orange',
  dcm: 'bg-accent-fill',
  other: 'bg-label-3',
};

interface CapacityPanelProps {
  title: string;
  // headline is the figure the panel is about; sub says what it is out of.
  headline: string;
  sub: string;
  capacity: Capacity;
  format: (value: number) => string;
}

// CapacityPanel is one host resource: its headline figure, then a capacity
// bar of what holds it and what is free, each part named with its amount.
export function CapacityPanel({ title, headline, sub, capacity, format }: CapacityPanelProps) {
  const t = useT();
  const parts: MeterPart[] = capacity.parts.map((part) => ({
    value: part.value,
    className: partFill[part.key],
    label: `${t(`admin.monitoring.part.${part.key}`)} ${format(part.value)}${part.key === 'lent' && part.value > 0 ? ` · ${t('admin.monitoring.lentNote')}` : ''}`,
  }));
  return (
    <section className="grid min-w-0 content-start gap-3">
      <div>
        <h3 className="text-[12px] font-semibold text-label-2">{title}</h3>
        <div className="mt-1 flex flex-wrap items-baseline gap-x-1.5">
          <span className="text-[24px] font-[650] tabular-nums tracking-[-0.02em] text-label">{headline}</span>
          <span className="text-label-2">{sub}</span>
        </div>
      </div>
      <Meter parts={parts} total={capacity.total} freeLabel={`${t('admin.monitoring.part.free')} ${format(capacity.free)}`} />
    </section>
  );
}
