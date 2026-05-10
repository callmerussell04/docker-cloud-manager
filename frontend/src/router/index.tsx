import { createBrowserRouter, Navigate, Outlet } from 'react-router-dom';
import { ProtectedRoute } from './ProtectedRoute';
import { LoginPage } from '@/pages/auth/LoginPage';
import { RegisterPage } from '@/pages/auth/RegisterPage';
import { AuthCallbackPage } from '@/pages/auth/AuthCallbackPage';
import { DashboardPage } from '@/pages/dashboard/DashboardPage';
import { ContainersPage } from '@/pages/containers/ContainersPage';
import { ContainerDetailsPage } from '@/pages/containers/ContainerDetailsPage';
import { ImagesPage } from '@/pages/images/ImagesPage';
import { VolumesPage } from '@/pages/volumes/VolumesPage';
import { ProjectsPage } from '@/pages/projects/ProjectsPage';
import { SystemSettingsPage } from '@/pages/admin/SystemSettingsPage';
import { AllResourcesPage } from '@/pages/admin/AllResourcesPage';
import { AdminUsersPage } from '@/pages/admin/AdminUsersPage';
import { AdminMonitoringPage } from '@/pages/admin/AdminMonitoringPage';
import { useAuthStore } from '@/store/authStore';

// eslint-disable-next-line react-refresh/only-export-components
function AdminRoute() {
  const role = useAuthStore(state => state.role);
  if (role !== 'admin') {
    return <Navigate to="/" replace />;
  }
  return <Outlet />;
}

export const router = createBrowserRouter([
  {
    path: '/',
    element: <ProtectedRoute />,
    children: [
      {
        index: true,
        element: <DashboardPage />,
      },
      { path: 'containers', element: <ContainersPage /> },
      { path: 'containers/:id', element: <ContainerDetailsPage /> },
      { path: 'images', element: <ImagesPage /> },
      { path: 'volumes', element: <VolumesPage /> },
      { path: 'projects', element: <ProjectsPage /> },
      
      {
        path: 'admin',
        element: <AdminRoute />,
        children: [
          { path: 'resources', element: <AllResourcesPage /> },
          { path: 'monitoring', element: <AdminMonitoringPage /> },
          { path: 'users', element: <AdminUsersPage /> },
          { path: 'settings', element: <SystemSettingsPage /> },
          { path: 'containers/:id', element: <ContainerDetailsPage /> },
        ]
      }
    ],
  },
  { path: '/login', element: <LoginPage /> },
  { path: '/auth/callback', element: <AuthCallbackPage /> },
  { path: '/register', element: <RegisterPage /> },
]);
