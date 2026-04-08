import { createBrowserRouter } from 'react-router-dom';
import { ProtectedRoute } from './ProtectedRoute';
import { LoginPage } from '@/pages/auth/LoginPage';
import { RegisterPage } from '@/pages/auth/RegisterPage';
import { ContainersPage } from '@/pages/containers/ContainersPage';

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
                Здесь будет статистика (память, диск, количество контейнеров)
              </p>
            </div>
          </div>
        ),
      },
      {
        path: 'containers',
        element: <ContainersPage />,
      },
      {
        path: 'images',
        element: <div>Образы и Сборка</div>,
      },
      {
        path: 'volumes',
        element: <div>Тома</div>,
      },
      {
        path: 'projects',
        element: <div>Docker Compose</div>,
      },
    ],
  },
  {
    path: '/login',
    element: <LoginPage />,
  },
  {
    path: '/register',
    element: <RegisterPage />,
  },
]);