import { create } from 'zustand';
import { notificationLifetimeMs, type NotificationKind } from './notification';

export interface Notification {
  id: number;
  kind: NotificationKind;
  title: string;
  // reason is the one short line an error carries under its title.
  reason?: string;
}

interface NotificationState {
  notifications: Notification[];
  notify: (title: string, kind?: NotificationKind, reason?: string) => void;
  dismiss: (id: number) => void;
}

let nextId = 0;

// useNotificationStore holds the notifications on screen. A notification the
// same as one already shown replaces it, so a repeated failure does not stack.
export const useNotificationStore = create<NotificationState>((set, get) => ({
  notifications: [],
  notify: (title, kind = 'info', reason) => {
    const id = ++nextId;
    set((state) => ({
      notifications: [
        ...state.notifications.filter((n) => n.title !== title || n.kind !== kind || n.reason !== reason),
        { id, kind, title, reason },
      ],
    }));
    const lifetime = notificationLifetimeMs(kind);
    if (lifetime !== null) window.setTimeout(() => get().dismiss(id), lifetime);
  },
  dismiss: (id) => set((state) => ({ notifications: state.notifications.filter((n) => n.id !== id) })),
}));
