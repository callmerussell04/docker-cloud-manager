import { useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useDashboardStats } from '@/features/dashboard/queries';
import { MIB } from '@/features/volumes/model';
import { VolumesPage } from '@/features/volumes/pages/VolumesPage';
import { createDialogFlag } from '@/shared/lib/hooks';

// VolumesRoute gives the create volume dialog the user's disk quota from the
// dashboard's figures, read only while the dialog is open. The volumes
// feature may not read the dashboard, so the app layer joins them.
export function VolumesRoute() {
  const [search] = useSearchParams();
  const { data: stats } = useDashboardStats(search.has(createDialogFlag));
  const disk = useMemo(
    () => (stats ? { usedBytes: stats.disk_used_mb * MIB, quotaBytes: stats.disk_quota_mb * MIB } : undefined),
    [stats],
  );
  return <VolumesPage disk={disk} />;
}
