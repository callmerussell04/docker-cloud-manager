import { Navigate, Outlet } from 'react-router-dom';
import { useAuthStore } from '@/shared/api/sessionStore';

export function AdminRoute() {
  const role = useAuthStore((state) => state.role);
  if (role !== 'admin') {
    return <Navigate to="/" replace />;
  }
  return <Outlet />;
}
