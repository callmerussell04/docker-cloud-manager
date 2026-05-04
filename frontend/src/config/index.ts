function requireViteEnv(name: 'VITE_API_URL' | 'VITE_BASE_DOMAIN') {
  const value = import.meta.env[name];
  if (!value) {
    throw new Error(`Missing required frontend env variable: ${name}`);
  }
  return value;
}

export const API_URL = requireViteEnv('VITE_API_URL');
export const BASE_DOMAIN = requireViteEnv('VITE_BASE_DOMAIN');
export const WS_URL = API_URL.replace(/^http/, 'ws');
