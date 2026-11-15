import type { adminResources as ru } from '../ru/adminResources';

export const adminResources: Record<keyof typeof ru, string> = {
  'admin.resources.title': 'All system resources',
  'admin.resources.subtitle': 'Manage resources for all platform users',
  'admin.resources.done': 'Action completed',
  'admin.resources.actionFailed': 'Failed to perform action',
  'admin.resources.volumeDeleted': 'Volume deleted',
  'admin.resources.imageDeleted': 'Image deleted',
  'admin.resources.buildDeleted': 'Build deleted',
  'admin.resources.owner': 'User',
  'admin.resources.buildId': 'Build ID',
  'admin.resources.imageId': 'Image ID',
  'admin.resources.userLabel': 'User: {value}',
  'admin.resources.dockerIdLabel': 'Docker ID: {value}',
};
