import type { PageParams } from './pagination';

export const queryKeys = {
  auth: {
    config: ['auth', 'config'] as const,
  },
  dashboard: {
    stats: ['dashboard', 'stats'] as const,
  },
  containers: {
    all: ['containers'] as const,
    lists: () => [...queryKeys.containers.all, 'list'] as const,
    list: (params: PageParams) => [...queryKeys.containers.lists(), params] as const,
    stats: (id: string, isAdmin: boolean) => [...queryKeys.containers.all, 'stats', id, isAdmin] as const,
  },
  volumes: {
    all: ['volumes'] as const,
    lists: () => [...queryKeys.volumes.all, 'list'] as const,
    list: (params: PageParams) => [...queryKeys.volumes.lists(), params] as const,
  },
  images: {
    all: ['images'] as const,
    lists: () => [...queryKeys.images.all, 'list'] as const,
    list: (params: PageParams) => [...queryKeys.images.lists(), params] as const,
    availability: ['images', 'buildAvailability'] as const,
  },
  builds: {
    all: ['builds'] as const,
    lists: () => [...queryKeys.builds.all, 'list'] as const,
    list: (params: PageParams) => [...queryKeys.builds.lists(), params] as const,
    logs: (id: string | undefined, isAdmin: boolean) => [...queryKeys.builds.all, 'logs', id, isAdmin] as const,
  },
  projects: {
    all: ['projects'] as const,
    lists: () => [...queryKeys.projects.all, 'list'] as const,
    list: (params: PageParams) => [...queryKeys.projects.lists(), params] as const,
  },
  admin: {
    users: {
      all: ['admin', 'users'] as const,
      lists: () => [...queryKeys.admin.users.all, 'list'] as const,
      list: (params: PageParams) => [...queryKeys.admin.users.lists(), params] as const,
    },
    monitoring: ['admin', 'monitoring'] as const,
    systemConfig: ['admin', 'systemConfig'] as const,
    reports: {
      all: ['admin', 'reports'] as const,
      overview: (params: unknown) => [...queryKeys.admin.reports.all, 'overview', params] as const,
      users: (params: unknown) => [...queryKeys.admin.reports.all, 'users', params] as const,
      userTimeline: (ownerId: string, params: unknown) => [...queryKeys.admin.reports.all, 'users', ownerId, 'timeline', params] as const,
      audit: (params: unknown) => [...queryKeys.admin.reports.all, 'audit', params] as const,
    },
    containers: {
      all: ['admin', 'containers'] as const,
      lists: () => [...queryKeys.admin.containers.all, 'list'] as const,
      list: (params: PageParams) => [...queryKeys.admin.containers.lists(), params] as const,
    },
    volumes: {
      all: ['admin', 'volumes'] as const,
      lists: () => [...queryKeys.admin.volumes.all, 'list'] as const,
      list: (params: PageParams) => [...queryKeys.admin.volumes.lists(), params] as const,
    },
    images: {
      all: ['admin', 'images'] as const,
      lists: () => [...queryKeys.admin.images.all, 'list'] as const,
      list: (params: PageParams) => [...queryKeys.admin.images.lists(), params] as const,
    },
    builds: {
      all: ['admin', 'builds'] as const,
      lists: () => [...queryKeys.admin.builds.all, 'list'] as const,
      list: (params: PageParams) => [...queryKeys.admin.builds.lists(), params] as const,
    },
    projects: {
      all: ['admin', 'projects'] as const,
      lists: () => [...queryKeys.admin.projects.all, 'list'] as const,
      list: (params: PageParams) => [...queryKeys.admin.projects.lists(), params] as const,
    },
  },
} as const;
