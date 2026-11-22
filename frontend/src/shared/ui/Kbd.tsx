import type { ReactNode } from 'react';

// Kbd is a keycap beside a control, such as the shortcut that opens the search
// palette. It is a hint for the eye only: the control names its shortcut to
// assistive technology itself (aria-keyshortcuts).
export function Kbd({ children }: { children: ReactNode }) {
  return <kbd aria-hidden className="rounded-[5px] bg-fill px-[5px] font-sans text-[11px] font-medium leading-[17px] text-label-2">{children}</kbd>;
}
