import { AlertTriangle, CheckCircle2, HelpCircle, XCircle, type LucideIcon } from 'lucide-react';
import { useT, type TFunction } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { buildSlotsInUse, healthChecks, type HealthCheck, type HealthMark, type SystemMonitoring } from '../model';

const marks: Record<HealthMark, { icon: LucideIcon; className: string }> = {
  ok: { icon: CheckCircle2, className: 'text-green' },
  warn: { icon: AlertTriangle, className: 'text-orange' },
  bad: { icon: XCircle, className: 'text-red' },
  unknown: { icon: HelpCircle, className: 'text-label-3' },
};

const admissionReasons = ['host_memory_exhausted', 'host_cpu_exhausted', 'host_disk_floor'] as const;

// admissionDetail names the host limits admission meets, translated. A
// reason this page does not know is left out rather than shown raw.
function admissionDetail(t: TFunction, reasons: readonly string[]) {
  return admissionReasons
    .filter((reason) => reasons.includes(reason))
    .map((reason) => t(`admin.monitoring.reason.${reason}`))
    .join(', ');
}

// checkText is a check's title and its one line of explanation.
function checkText(t: TFunction, check: HealthCheck, stats: SystemMonitoring): [string, string] {
  switch (check.id) {
    case 'admission':
      return check.state === 'open'
        ? [t('admin.monitoring.health.admission.open'), t('admin.monitoring.health.admission.openDetail')]
        : [t('admin.monitoring.health.admission.blocked'), admissionDetail(t, stats.admission_reasons)];
    case 'containerDisk':
      return [t(`admin.monitoring.health.containerDisk.${check.state}`), t(`admin.monitoring.health.containerDisk.${check.state}Detail`)];
    case 'volumeSizes':
      return [t(`admin.monitoring.health.volumeSizes.${check.state}`), t(`admin.monitoring.health.volumeSizes.${check.state}Detail`)];
    case 'builds':
      return [
        t('admin.monitoring.health.builds.inUse', { used: buildSlotsInUse(stats), max: stats.max_concurrent_builds }),
        check.state === 'free'
          ? t('admin.monitoring.health.builds.freeDetail')
          : t('admin.monitoring.health.builds.heldByDiskDetail', { cap: stats.build_claim_cap, max: stats.max_concurrent_builds }),
      ];
  }
}

// HealthList is the host's health checks, each a mark beside its title and a
// line of explanation, so the mark's colour never stands alone.
export function HealthList({ stats }: { stats: SystemMonitoring }) {
  const t = useT();
  return (
    <ul className="grid gap-3.5">
      {healthChecks(stats).map((check) => {
        const { icon: Icon, className } = marks[check.mark];
        const [title, detail] = checkText(t, check, stats);
        return (
          <li key={check.id} className="flex items-start gap-2.5">
            <Icon aria-hidden className={cn('mt-px h-[17px] w-[17px] shrink-0', className)} strokeWidth={2} />
            <div className="min-w-0">
              <div className={cn('font-semibold', check.mark === 'bad' ? 'text-red-text' : 'text-label')}>{title}</div>
              {detail && <div className="mt-0.5 text-[12px] text-label-2">{detail}</div>}
            </div>
          </li>
        );
      })}
    </ul>
  );
}
