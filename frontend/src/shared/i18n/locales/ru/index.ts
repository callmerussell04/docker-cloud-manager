import { common } from './common';
import { validation } from './validation';
import { status } from './status';
import { apiError } from './apiError';
import { auth } from './auth';
import { dashboard } from './dashboard';
import { containers } from './containers';
import { images } from './images';
import { volumes } from './volumes';
import { projects } from './projects';
import { adminUsers } from './adminUsers';
import { adminSettings } from './adminSettings';
import { adminMonitoring } from './adminMonitoring';
import { adminReports } from './adminReports';
import { adminResources } from './adminResources';

// The Russian dictionary is the source of the translation key type.
export const ru = {
  ...common,
  ...validation,
  ...status,
  ...apiError,
  ...auth,
  ...dashboard,
  ...containers,
  ...images,
  ...volumes,
  ...projects,
  ...adminUsers,
  ...adminSettings,
  ...adminMonitoring,
  ...adminReports,
  ...adminResources,
} as const;
