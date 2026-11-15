export interface AuthResponse {
  access_token?: string;
  user_id?: string;
}

export interface AuthProviderData {
  name: string;
}

export interface AuthConfig {
  local_login_enabled: boolean;
  local_register_enabled: boolean;
  oidc_providers: AuthProviderData[];
}
