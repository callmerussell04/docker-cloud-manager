// StatusTone is what a state means to the user: working, failing, changing,
// or at rest.
export type StatusTone = 'ok' | 'bad' | 'busy' | 'idle';

// statusToneRank is a list's ascending status order: failing first, then
// changing, working and at rest.
export const statusToneRank: Readonly<Record<StatusTone, number>> = { bad: 0, busy: 1, ok: 2, idle: 3 };
