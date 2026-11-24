import { useT } from '@/shared/i18n';
import { ageOf } from '@/shared/lib/age';
import { useNowSeconds } from '@/shared/lib/hooks';
import type { AuditEvent } from '../model';

// useActorName names who did an action: their username, or the system.
export function useActorName() {
  const t = useT();
  return (event: AuditEvent) => event.actor_username || (event.actor_user_id ? event.actor_user_id : t('admin.reports.systemActor'));
}

// resourceName is what an action touched, by name where it was recorded.
export function resourceName(event: AuditEvent) {
  return event.resource_name || event.resource_id || event.resource_type;
}

// useAge writes how long ago a time was, as "12 min ago".
export function useAge() {
  const t = useT();
  const short = useShortAge();
  return (time: number) => t('admin.reports.ago', { age: short(time) });
}

// useShortAge writes how long ago a time was in its short form, as a list's
// age column does ("12 min").
export function useShortAge() {
  const t = useT();
  const now = useNowSeconds();
  return (time: number) => {
    const age = ageOf(time, now);
    return t(`common.age.${age.unit}`, { count: age.count });
  };
}
