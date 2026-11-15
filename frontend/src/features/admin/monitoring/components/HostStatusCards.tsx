import { HardDrive } from 'lucide-react';

import { Badge } from '@/shared/ui/Badge';
import { Card, CardContent } from '@/shared/ui/Card';
import { useT } from '@/shared/i18n';
import type { RootfsQuotaStatus, VolumeSizeMode } from '../model';

const rootfsQuotaBadge: Record<RootfsQuotaStatus, 'success' | 'warning' | 'default'> = {
  enforced: 'success',
  not_enforced: 'warning',
  unknown: 'default',
};

export function RootfsQuotaCard({ status }: { status: RootfsQuotaStatus }) {
  const t = useT();
  const known = status in rootfsQuotaBadge ? status : 'unknown';
  return (
    <Card>
      <CardContent className="p-5 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3 min-w-0">
          <HardDrive className="w-5 h-5 shrink-0 text-slate-500 dark:text-slate-400" />
          <div className="min-w-0">
            <p className="font-medium text-slate-900 dark:text-slate-100">{t('admin.monitoring.rootfsQuota')}</p>
            <p className="text-xs text-slate-500 dark:text-slate-400">{t('admin.monitoring.rootfsQuotaHint')}</p>
          </div>
        </div>
        <Badge variant={rootfsQuotaBadge[known]} className="self-start sm:self-center shrink-0">
          {t(`admin.monitoring.rootfsQuota.${known}`)}
        </Badge>
      </CardContent>
    </Card>
  );
}

const volumeSizeModeBadge: Record<VolumeSizeMode, 'success' | 'warning' | 'default'> = {
  enforced: 'success',
  measured: 'default',
  unavailable: 'warning',
};

export function VolumeSizeModeCard({ mode }: { mode: VolumeSizeMode }) {
  const t = useT();
  const known = mode in volumeSizeModeBadge ? mode : 'measured';
  return (
    <Card>
      <CardContent className="p-5 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3 min-w-0">
          <HardDrive className="w-5 h-5 shrink-0 text-slate-500 dark:text-slate-400" />
          <div className="min-w-0">
            <p className="font-medium text-slate-900 dark:text-slate-100">{t('admin.monitoring.volumeSizeMode')}</p>
            <p className="text-xs text-slate-500 dark:text-slate-400">{t(`admin.monitoring.volumeSizeMode.${known}.hint`)}</p>
          </div>
        </div>
        <Badge variant={volumeSizeModeBadge[known]} className="self-start sm:self-center shrink-0">
          {t(`admin.monitoring.volumeSizeMode.${known}`)}
        </Badge>
      </CardContent>
    </Card>
  );
}
