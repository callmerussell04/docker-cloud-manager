import { lazy, Suspense, type ComponentType, type ReactNode } from 'react';
import { createBrowserRouter, type RouteObject } from 'react-router-dom';
import { ProtectedRoute } from './guards/ProtectedRoute';
import { AdminRoute } from './guards/AdminRoute';
import { PageFallback } from './layout/PageFallback';
import { RouteError } from './layout/RouteError';

// page loads a page module only when its route is first rendered. Each loader
// imports the page module directly so that every page becomes its own chunk.
function page<P extends object, K extends string>(load: () => Promise<Record<K, ComponentType<P>>>, name: K) {
  return lazy(async () => ({ default: (await load())[name] }));
}

const LoginPage = page(() => import('@/features/auth/pages/LoginPage'), 'LoginPage');
const RegisterPage = page(() => import('@/features/auth/pages/RegisterPage'), 'RegisterPage');
const AuthCallbackPage = page(() => import('@/features/auth/pages/AuthCallbackPage'), 'AuthCallbackPage');
const DashboardPage = page(() => import('@/features/dashboard/pages/DashboardPage'), 'DashboardPage');
const ContainersRoute = page(() => import('@/app/routes/ContainersRoute'), 'ContainersRoute');
const ContainerRoute = page(() => import('@/app/routes/ContainerRoute'), 'ContainerRoute');
const ImagesPage = page(() => import('@/features/images/pages/ImagesPage'), 'ImagesPage');
const BuildPage = page(() => import('@/features/images/pages/BuildPage'), 'BuildPage');
const VolumesRoute = page(() => import('@/app/routes/VolumesRoute'), 'VolumesRoute');
const ProjectsPage = page(() => import('@/features/projects/pages/ProjectsPage'), 'ProjectsPage');
const ProjectPage = page(() => import('@/features/projects/pages/ProjectPage'), 'ProjectPage');
const AllResourcesPage = page(() => import('@/features/admin/resources/pages/AllResourcesPage'), 'AllResourcesPage');
const AdminMonitoringPage = page(() => import('@/features/admin/monitoring/pages/AdminMonitoringPage'), 'AdminMonitoringPage');
const AdminReportsPage = page(() => import('@/features/admin/reports/pages/AdminReportsPage'), 'AdminReportsPage');
const AdminUsersPage = page(() => import('@/features/admin/users/pages/AdminUsersPage'), 'AdminUsersPage');
const SystemSettingsPage = page(() => import('@/features/admin/settings/pages/SystemSettingsPage'), 'SystemSettingsPage');

// Pages outside the signed-in layout carry their own loading boundary.
function standalone(element: ReactNode) {
  return <Suspense fallback={<PageFallback fullScreen />}>{element}</Suspense>;
}

// A page that fails keeps the layout around it: every page route shows
// RouteError in its own place, and the outer routes show it full screen.
function withPageErrors(routes: RouteObject[]): RouteObject[] {
  return routes.map((route) => ({
    ...route,
    errorElement: <RouteError />,
    children: route.children ? withPageErrors(route.children) : undefined,
  } as RouteObject));
}

export const router = createBrowserRouter([
  {
    path: '/',
    element: <ProtectedRoute />,
    errorElement: <RouteError fullScreen />,
    children: withPageErrors([
      { index: true, element: <DashboardPage /> },
      { path: 'containers', element: <ContainersRoute /> },
      { path: 'containers/:id', element: <ContainerRoute scope="own" backTo="/containers" backLabel="nav.containers" /> },
      { path: 'images', element: <ImagesPage /> },
      { path: 'images/builds/:id', element: <BuildPage scope="own" backTo="/images?tab=builds" backLabel="images.backToBuilds" /> },
      { path: 'volumes', element: <VolumesRoute /> },
      { path: 'projects', element: <ProjectsPage /> },
      { path: 'projects/:id', element: <ProjectPage scope="own" backTo="/projects" backLabel="nav.projects" containersPath="/containers" /> },
      {
        path: 'admin',
        element: <AdminRoute />,
        children: [
          { path: 'resources', element: <AllResourcesPage /> },
          { path: 'monitoring', element: <AdminMonitoringPage /> },
          { path: 'reports', element: <AdminReportsPage /> },
          { path: 'users', element: <AdminUsersPage /> },
          { path: 'settings', element: <SystemSettingsPage /> },
          { path: 'containers/:id', element: <ContainerRoute scope="all" backTo="/admin/resources" backLabel="nav.adminResources" /> },
        ],
      },
    ]),
  },
  { path: '/login', element: standalone(<LoginPage />), errorElement: <RouteError fullScreen /> },
  { path: '/auth/callback', element: standalone(<AuthCallbackPage />), errorElement: <RouteError fullScreen /> },
  { path: '/register', element: standalone(<RegisterPage />), errorElement: <RouteError fullScreen /> },
]);
