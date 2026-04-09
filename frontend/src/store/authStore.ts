import { create } from 'zustand';

interface AuthState {
  accessToken: string | null;
  role: 'admin' | 'user' | null;
  username: string | null;
  isInitialized: boolean;
  setAccessToken: (token: string | null) => void;
  setInitialized: (status: boolean) => void;
  logout: () => void;
}

function parseJwtPayload(token: string): { role: 'admin' | 'user' | null, username: string | null } {
  try {
    const base64Url = token.split('.')[1];
    const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
    const jsonPayload = decodeURIComponent(window.atob(base64).split('').map(function(c) {
      return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2);
    }).join(''));

    const payload = JSON.parse(jsonPayload);
    return { role: payload.role || 'user', username: payload.username || null };
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  } catch (e) {
    return { role: null, username: null };
  }
}

export const useAuthStore = create<AuthState>((set) => ({
  accessToken: null,
  role: null,
  username: null,
  isInitialized: false,
  setAccessToken: (token) => {
    if (token) {
      const { role, username } = parseJwtPayload(token);
      set({ accessToken: token, role, username });
    } else {
      set({ accessToken: null, role: null, username: null });
    }
  },
  setInitialized: (status) => set({ isInitialized: status }),
  logout: () => set({ accessToken: null, role: null, username: null }),
}));