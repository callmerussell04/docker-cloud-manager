import type { dashboard as ru } from '../ru/dashboard';

export const dashboard: Record<keyof typeof ru, string> = {
  'dashboard.title': 'Dashboard',
  'dashboard.usedPercent': 'Used {percent}%',
  'dashboard.ram': 'Memory (RAM) (Reserved)',
  'dashboard.cpu': 'CPU (Reserved)',
  'dashboard.cores': '{cores} vCPU',
  'dashboard.disk': 'Disk space (images and volume sizes)',
  'dashboard.containerLimit': 'Container limit',
  'dashboard.volumeLimit': 'Volume limit',
  'dashboard.runningContainers': 'Running containers',
  'dashboard.customImages': 'Custom images',
  'dashboard.composeProjects': 'Compose projects',
  'dashboard.loadFailed': 'Failed to load statistics',
};
