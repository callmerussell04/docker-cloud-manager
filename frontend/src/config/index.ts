export const API_URL = import.meta.env.VITE_API_URL;
export const BASE_DOMAIN = import.meta.env.VITE_BASE_DOMAIN;
export const WS_URL = API_URL.replace(/^http/, 'ws');