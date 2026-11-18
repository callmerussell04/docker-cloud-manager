import { Cpu } from 'lucide-react';

import { Card, CardContent, CardHeader, CardTitle } from '@/shared/ui/Card';
import { useT } from '@/shared/i18n';
import { millicoresToCores } from '@/features/admin/settings/model';

/** The CPU reserving containers and occupied build slots hold, in cores. */
export function ReservedCPUCard({ containerMillicores, buildMillicores }: { containerMillicores: number; buildMillicores: number }) {
  const t = useT();
  const rows = [
    { label: t('admin.monitoring.reservedCPUContainers'), millicores: containerMillicores },
    { label: t('admin.monitoring.reservedCPUBuilds'), millicores: buildMillicores },
  ];

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-lg flex items-center gap-2">
          <Cpu className="w-5 h-5 text-indigo-500" />
          {t('admin.monitoring.reservedCPU')}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          {rows.map((row) => (
            <div key={row.label}>
              <dt className="text-sm text-slate-500 dark:text-slate-400">{row.label}</dt>
              <dd className="text-2xl font-semibold text-slate-900 dark:text-slate-100">
                {t('admin.monitoring.cores', { value: millicoresToCores(row.millicores) })}
              </dd>
            </div>
          ))}
        </dl>
      </CardContent>
    </Card>
  );
}
