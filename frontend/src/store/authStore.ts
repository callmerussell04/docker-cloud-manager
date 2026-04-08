import { create } from 'zustand';

interface AuthState {
  accessToken: string | null;
  role: 'admin' | 'user' | null; // НОВОЕ
  isInitialized: boolean;
  setAccessToken: (token: string | null) => void;
  setInitialized: (status: boolean) => void;
  logout: () => void;
}

// Простая функция для декодирования JWT на клиенте
function parseJwtRole(token: string): 'admin' | 'user' | null {
  try {
    const base64Url = token.split('.')[1];
    const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
    const jsonPayload = decodeURIComponent(window.atob(base64).split('').map(function(c) {
      return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2);
    }).join(''));

    const payload = JSON.parse(jsonPayload);
    return payload.role || 'user';
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  } catch (e) {
    return null;
  }
}

export const useAuthStore = create<AuthState>((set) => ({
  accessToken: null,
  role: null,
  isInitialized: false,
  setAccessToken: (token) => {
    if (token) {
      set({ accessToken: token, role: parseJwtRole(token) });
    } else {
      set({ accessToken: null, role: null });
    }
  },
  setInitialized: (status) => set({ isInitialized: status }),
  logout: () => set({ accessToken: null, role: null }),
}));