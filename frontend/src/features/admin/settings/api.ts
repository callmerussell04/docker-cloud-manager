import { privateApi } from '@/shared/api/client';
import type { SystemConfig } from './model';

export const getSystemConfigFn = async (): Promise<SystemConfig> => {
  const response = await privateApi.get<SystemConfig>('/admin/config');
  return normalizeSystemConfig(response.data);
};

export const updateSystemConfigFn = async (data: SystemConfig): Promise<void> => {
  await privateApi.put('/admin/config', data);
};

function normalizeSystemConfig(data: SystemConfig): SystemConfig {
  return {
    ...data,
    reserved_domain_prefixes: data.reserved_domain_prefixes ?? [],
    blocked_domain_prefix_patterns: data.blocked_domain_prefix_patterns ?? [],
    git_allowed_hosts: data.git_allowed_hosts ?? [],
    telemetry_allowed_exec_commands: data.telemetry_allowed_exec_commands ?? [],
  };
}
