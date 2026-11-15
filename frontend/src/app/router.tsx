import { lazy, Suspense, type ComponentType, type ReactNode } from 'react';
import { createBrowserRouter } from 'react-router-dom';
import { ProtectedRoute } from './guards/ProtectedRoute';
import { AdminRoute } from './guards/AdminRoute';
import { PageFallback } from './layout/PageFallback';

// page loads a page module only when its route is first rendered. Each loader
// imports the page module directly so that every page becomes its own chunk.
function page<P extends object, K extends string>(load: () => Promise<Record<K, ComponentType<P>>>, name: K) {
  return lazy(async () => ({ default: (await load())[name] }));
}

const LoginPage = page(() => import('@/features/auth/pages/LoginPage'), 'LoginPage');
const RegisterPage = page(() => import('@/features/auth/pages/RegisterPage'), 'RegisterPage');
const AuthCallbackPage = page(() => import('@/features/auth/pages/AuthCallbackPage'), 'AuthCallbackPage');
const DashboardPage = page(() => import('@/features/dashboard/pages/DashboardPage'), 'DashboardPage');
const ContainersPage = page(() => import('@/features/containers/pages/ContainersPage'), 'ContainersPage');
const ContainerDetailsPage = page(() => import('@/features/containers/pages/ContainerDetailsPage'), 'ContainerDetailsPage');
const ImagesPage = page(() => import('@/features/images/pages/ImagesPage'), 'ImagesPage');
const VolumesPage = page(() => import('@/features/volumes/pages/VolumesPage'), 'VolumesPage');
const ProjectsPage = page(() => import('@/features/projects/pages/ProjectsPage'), 'ProjectsPage');
const AllResourcesPage = page(() => import('@/features/admin/resources/pages/AllResourcesPage'), 'AllResourcesPage');
const AdminMonitoringPage = page(() => import('@/features/admin/monitoring/pages/AdminMonitoringPage'), 'AdminMonitoringPage');
const AdminReportsPage = page(() => import('@/features/admin/reports/pages/AdminReportsPage'), 'AdminReportsPage');
const AdminUsersPage = page(() => import('@/features/admin/users/pages/AdminUsersPage'), 'AdminUsersPage');
const SystemSettingsPage = page(() => import('@/features/admin/settings/pages/SystemSettingsPage'), 'SystemSettingsPage');

// Pages outside the signed-in layout carry their own loading boundary.
function standalone(element: ReactNode) {
  return <Suspense fallback={<PageFallback fullScreen />}>{element}</Suspense>;
}

export const router = createBrowserRouter([
  {
    path: '/',
    element: <ProtectedRoute />,
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'containers', element: <ContainersPage /> },
      { path: 'containers/:id', element: <ContainerDetailsPage scope="own" backTo="/containers" /> },
      { path: 'images', element: <ImagesPage /> },
      { path: 'volumes', element: <VolumesPage /> },
      { path: 'projects', element: <ProjectsPage /> },
      {
        path: 'admin',
        element: <AdminRoute />,
        children: [
          { path: 'resources', element: <AllResourcesPage /> },
          { path: 'monitoring', element: <AdminMonitoringPage /> },
          { path: 'reports', element: <AdminReportsPage /> },
          { path: 'users', element: <AdminUsersPage /> },
          { path: 'settings', element: <SystemSettingsPage /> },
          { path: 'containers/:id', element: <ContainerDetailsPage scope="all" backTo="/admin/resources" /> },
        ],
      },
    ],
  },
  { path: '/login', element: standalone(<LoginPage />) },
  { path: '/auth/callback', element: standalone(<AuthCallbackPage />) },
  { path: '/register', element: standalone(<RegisterPage />) },
]);
