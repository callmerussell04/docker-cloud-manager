export type AgeUnit = 'minute' | 'hour' | 'day' | 'month' | 'year';

export interface Age {
  count: number;
  unit: AgeUnit;
}

const minute = 60;
const hour = 60 * minute;
const day = 24 * hour;

// ageOf is how long ago a Unix time was, in its largest whole unit, as a
// list's age column shows it ("42 min", "12 d"): minutes below an hour, hours
// below a day, days below 60 days, months below a year, then years. Anything
// under two minutes, or in the future, is one minute.
export function ageOf(time: number, now: number): Age {
  const seconds = Math.max(0, now - time);
  if (seconds < hour) return { count: Math.max(1, Math.floor(seconds / minute)), unit: 'minute' };
  if (seconds < day) return { count: Math.floor(seconds / hour), unit: 'hour' };
  if (seconds < 60 * day) return { count: Math.floor(seconds / day), unit: 'day' };
  if (seconds < 365 * day) return { count: Math.floor(seconds / (30 * day)), unit: 'month' };
  return { count: Math.floor(seconds / (365 * day)), unit: 'year' };
}
