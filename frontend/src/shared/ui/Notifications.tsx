import { createPortal } from 'react-dom';
import { CheckCircle2, Info, X, XCircle, type LucideIcon } from 'lucide-react';
import { cn } from '@/shared/lib/utils';
import { useT } from '@/shared/i18n';
import { useSizeClass } from '@/shared/lib/useSizeClass';
import { Glass } from './Glass';
import type { NotificationKind } from './notification';
import { useNotificationStore } from './notificationStore';

const icons: Record<NotificationKind, { icon: LucideIcon; className: string }> = {
  success: { icon: CheckCircle2, className: 'text-green' },
  error: { icon: XCircle, className: 'text-red' },
  info: { icon: Info, className: 'text-label-3' },
};

// Notifications are short glass pills at the bottom centre, above the phone
// tab bar: the title, and for an error its reason on a second line. Each can
// be dismissed; which ones leave on their own is notificationLifetimeMs.
export function Notifications() {
  const t = useT();
  const notifications = useNotificationStore((state) => state.notifications);
  const dismiss = useNotificationStore((state) => state.dismiss);
  const phone = useSizeClass() === 'phone';

  return createPortal(
    <div
      className={cn(
        'pointer-events-none fixed left-1/2 z-[200] grid w-[min(440px,calc(100vw-24px))] -translate-x-1/2 justify-items-center gap-2',
        phone ? 'bottom-[88px]' : 'bottom-6',
      )}
      aria-live="polite"
    >
      {notifications.map((notification) => {
        const { icon: Icon, className } = icons[notification.kind];
        return (
          <Glass
            key={notification.id}
            material="dense"
            role={notification.kind === 'error' ? 'alert' : 'status'}
            className={cn(
              'pointer-events-auto flex max-w-full gap-2.5 py-1.5 pl-3.5 pr-1.5 text-label animate-in fade-in slide-in-from-bottom-2',
              notification.reason ? 'items-start rounded-[18px] py-2.5' : 'min-h-[44px] items-center rounded-full',
            )}
          >
            <Icon className={cn('h-[17px] w-[17px] shrink-0', notification.reason && 'mt-px', className)} />
            <span className="grid min-w-0 flex-1 gap-0.5">
              <b className="break-words text-[13px] font-semibold leading-5">{notification.title}</b>
              {notification.reason && <span className="break-words text-xs leading-snug text-label-2">{notification.reason}</span>}
            </span>
            <button
              type="button"
              onClick={() => dismiss(notification.id)}
              className={cn('flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-label-2 hover:bg-fill hover:text-label', notification.reason && '-mt-1')}
              aria-label={t('common.dismissNotification')}
              title={t('common.dismissNotification')}
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </Glass>
        );
      })}
    </div>,
    document.body,
  );
}
