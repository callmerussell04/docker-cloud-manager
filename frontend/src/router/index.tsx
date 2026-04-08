import { createBrowserRouter, Navigate, Outlet } from 'react-router-dom';
import { ProtectedRoute } from './ProtectedRoute';
import { LoginPage } from '@/pages/auth/LoginPage';
import { RegisterPage } from '@/pages/auth/RegisterPage';
import { ContainersPage } from '@/pages/containers/ContainersPage';
import { ImagesPage } from '@/pages/images/ImagesPage';
import { VolumesPage } from '@/pages/volumes/VolumesPage';
import { ProjectsPage } from '@/pages/projects/ProjectsPage';

// Импорты страниц администратора
import { SystemSettingsPage } from '@/pages/admin/SystemSettingsPage';
import { AllResourcesPage } from '@/pages/admin/AllResourcesPage';
import { useAuthStore } from '@/store/authStore';

// Защита админских роутов на клиенте
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
        element: (
          <div>
            <h1 className="text-3xl font-bold tracking-tight mb-6">Дашборд</h1>
            <div className="bg-white/40 dark:bg-slate-900/40 backdrop-blur-xl border border-white/50 dark:border-slate-700/50 rounded-2xl p-8 flex items-center justify-center min-h-[400px]">
              <p className="text-lg text-slate-500 dark:text-slate-400">
                Добро пожаловать в CloudManager. Здесь будет статистика в будущих обновлениях.
              </p>
            </div>
          </div>
        ),
      },
      { path: 'containers', element: <ContainersPage /> },
      { path: 'images', element: <ImagesPage /> },
      { path: 'volumes', element: <VolumesPage /> },
      { path: 'projects', element: <ProjectsPage /> },
      
      // Админские роуты
      {
        path: 'admin',
        element: <AdminRoute />,
        children: [
          { path: 'resources', element: <AllResourcesPage /> },
          { path: 'settings', element: <SystemSettingsPage /> },
        ]
      }
    ],
  },
  { path: '/login', element: <LoginPage /> },
  { path: '/register', element: <RegisterPage /> },
]);