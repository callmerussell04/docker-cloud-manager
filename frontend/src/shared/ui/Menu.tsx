import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { Check, type LucideIcon } from 'lucide-react';
import { cn } from '@/shared/lib/utils';
import { Glass } from './Glass';

// Where the menu opens against its trigger.
type MenuSide = 'above-start' | 'below-end';

interface MenuProps {
  // label names the menu, and the trigger too when the trigger shows only an
  // icon (iconOnly), as its accessible name and tooltip.
  label: string;
  iconOnly?: boolean;
  trigger: ReactNode;
  triggerClassName?: string;
  side?: MenuSide;
  children: ReactNode;
}

const MenuContext = createContext<() => void>(() => {});

const itemSelector = '[role^="menuitem"]:not(:disabled)';

function placement(side: MenuSide, anchor: DOMRect): CSSProperties {
  const gap = 6;
  if (side === 'above-start') return { left: anchor.left, bottom: window.innerHeight - anchor.top + gap, minWidth: anchor.width };
  return { right: window.innerWidth - anchor.right, top: anchor.bottom + gap };
}

// Menu is a glass popover anchored to its trigger. It renders at the end of
// the document, outside the glass that opens it, because a blur nested in a
// blur sees only its parent. Focus moves to the first item when it opens;
// arrow keys, Home and End move between items; Escape closes it and returns
// focus to the trigger; a click outside or Tab closes it.
export function Menu({ label, iconOnly = false, trigger, triggerClassName, side = 'below-end', children }: MenuProps) {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [style, setStyle] = useState<CSSProperties | null>(null);
  const open = style !== null;

  const close = useCallback((returnFocus = true) => {
    setStyle(null);
    if (returnFocus) triggerRef.current?.focus();
  }, []);

  const toggle = () => {
    const anchor = triggerRef.current?.getBoundingClientRect();
    if (open || !anchor) close(false);
    else setStyle(placement(side, anchor));
  };

  useLayoutEffect(() => {
    if (open) menuRef.current?.querySelector<HTMLElement>(itemSelector)?.focus();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (menuRef.current?.contains(target) || triggerRef.current?.contains(target)) return;
      close(false);
    };
    // The menu is placed once; a window that changes size would leave it
    // floating away from its trigger, so it closes instead.
    const onResize = () => close(false);
    // Escape closes the menu wherever focus is, even before it reaches an item.
    const onEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      event.stopPropagation();
      close();
    };
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onEscape);
    window.addEventListener('resize', onResize);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onEscape);
      window.removeEventListener('resize', onResize);
    };
  }, [open, close]);

  const onKeyDown = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const items = Array.from(menuRef.current?.querySelectorAll<HTMLElement>(itemSelector) ?? []);
    const index = items.indexOf(document.activeElement as HTMLElement);
    const focusAt = (next: number) => items[(next + items.length) % items.length]?.focus();
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault();
        focusAt(index + 1);
        break;
      case 'ArrowUp':
        event.preventDefault();
        focusAt(index < 0 ? -1 : index - 1);
        break;
      case 'Home':
        event.preventDefault();
        focusAt(0);
        break;
      case 'End':
        event.preventDefault();
        focusAt(-1);
        break;
      case 'Tab':
        close(false);
        break;
    }
  };

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className={triggerClassName}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={iconOnly ? label : undefined}
        title={iconOnly ? label : undefined}
        onClick={toggle}
      >
        {trigger}
      </button>
      {style &&
        createPortal(
          <MenuContext.Provider value={close}>
            <Glass
              ref={menuRef}
              material="dense"
              role="menu"
              aria-label={label}
              className="menu-pop fixed z-[80] min-w-[200px] rounded-[14px] p-[5px]"
              style={style}
              onKeyDown={onKeyDown}
            >
              {children}
            </Glass>
          </MenuContext.Provider>,
          document.body,
        )}
    </>
  );
}

interface MenuItemProps {
  icon?: LucideIcon;
  children: ReactNode;
  onSelect: () => void;
  // checked makes the item one choice of a MenuGroup, marked when chosen.
  checked?: boolean;
}

// MenuItem runs its action and closes the menu.
export function MenuItem({ icon: Icon, children, onSelect, checked }: MenuItemProps) {
  const close = useContext(MenuContext);
  const choice = checked !== undefined;
  return (
    <button
      type="button"
      role={choice ? 'menuitemradio' : 'menuitem'}
      aria-checked={choice ? checked : undefined}
      tabIndex={-1}
      onClick={() => {
        close();
        onSelect();
      }}
      className={cn(
        'group flex h-[30px] w-full items-center gap-[9px] rounded-[9px] px-[9px] text-left outline-none',
        'focus-visible:bg-accent-fill focus-visible:text-on-accent hover:bg-accent-fill hover:text-on-accent',
        'text-label',
      )}
    >
      {Icon && <Icon className="h-[15px] w-[15px] shrink-0 text-label-2 group-hover:text-on-accent group-focus-visible:text-on-accent" strokeWidth={1.8} />}
      <span className="flex-1 truncate">{children}</span>
      {choice && <Check className={cn('h-[14px] w-[14px] shrink-0', !checked && 'invisible')} strokeWidth={2.2} />}
    </button>
  );
}

export function MenuSeparator() {
  return <div role="separator" className="mx-[9px] my-[5px] h-px bg-separator" />;
}

// MenuGroup holds a set of choices under its label, so assistive technology
// reads each set as its own group.
export function MenuGroup({ label, children }: { label: string; children: ReactNode }) {
  const id = useId();
  return (
    <div role="group" aria-labelledby={id}>
      <div id={id} className="px-[9px] pb-1 pt-[7px] text-[11px] font-semibold text-label-2">{label}</div>
      {children}
    </div>
  );
}
