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
import { useT } from '@/shared/i18n';
import { useSizeClass } from '@/shared/lib/useSizeClass';
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
  // sheetTitle heads the menu when it opens as a sheet on a phone; it is the
  // label by default.
  sheetTitle?: string;
  children: ReactNode;
}

interface MenuState {
  close: () => void;
  // sheet says the menu opened as a sheet, whose items are larger for a thumb.
  sheet: boolean;
}

const MenuContext = createContext<MenuState>({ close: () => {}, sheet: false });

const itemSelector = '[role^="menuitem"]:not(:disabled)';

function placement(side: MenuSide, anchor: DOMRect): CSSProperties {
  const gap = 6;
  if (side === 'above-start') return { left: anchor.left, bottom: window.innerHeight - anchor.top + gap, minWidth: anchor.width };
  return { right: window.innerWidth - anchor.right, top: anchor.bottom + gap };
}

// Menu is a glass popover anchored to its trigger on wide and medium windows,
// and on a phone a sheet that rises from the bottom, with a title and Cancel,
// where a thumb reaches it. It renders at the end of the document, outside
// the glass that opens it, because a blur nested in a blur sees only its
// parent. Focus moves to the first item when it opens; arrow keys, Home and
// End move between items; Escape closes it and returns focus to the trigger;
// a click outside or Tab closes it.
export function Menu({ label, iconOnly = false, trigger, triggerClassName, side = 'below-end', sheetTitle, children }: MenuProps) {
  const t = useT();
  const sheet = useSizeClass() === 'phone';
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  // opened places an open popover, and records whether the menu opened as a
  // sheet, which sits at the bottom of the window. A menu opened as one kind
  // would be the wrong kind once the window crosses into or out of the phone
  // class, so it counts as closed then.
  const [opened, setOpened] = useState<{ style: CSSProperties; sheet: boolean } | null>(null);
  // Forget it then too, so crossing back does not reopen it by itself.
  if (opened !== null && opened.sheet !== sheet) setOpened(null);
  const style = opened !== null && opened.sheet === sheet ? opened.style : null;
  const open = style !== null;

  const close = useCallback((returnFocus = true) => {
    setOpened(null);
    if (returnFocus) triggerRef.current?.focus();
  }, []);

  const toggle = () => {
    const anchor = triggerRef.current?.getBoundingClientRect();
    if (open || !anchor) {
      close(false);
      return;
    }
    setOpened({ style: sheet ? {} : placement(side, anchor), sheet });
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
    // A popover is placed once; a window that changes size would leave it
    // floating away from its trigger, so it closes instead. A sheet stays at
    // the bottom of the window, and a phone resizes as its toolbars come and
    // go, so a sheet stays open.
    const onResize = () => {
      if (!sheet) close(false);
    };
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
  }, [open, sheet, close]);

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
          <MenuContext.Provider value={{ close, sheet }}>
            {sheet ? (
              <>
                <div aria-hidden className="sheet-scrim fixed inset-0 z-[79] bg-scrim" />
                <Glass
                  ref={menuRef}
                  material="dense"
                  role="menu"
                  aria-label={sheetTitle ?? label}
                  className="sheet-pop fixed inset-x-2 bottom-2 z-[80] max-h-[80vh] overflow-y-auto rounded-[28px] p-2"
                  onKeyDown={onKeyDown}
                >
                  <div aria-hidden className="truncate px-3.5 pb-2.5 pt-2 text-center text-[13px] font-semibold text-label-2">{sheetTitle ?? label}</div>
                  {children}
                  <button
                    type="button"
                    role="menuitem"
                    tabIndex={-1}
                    onClick={() => close()}
                    className="mt-1.5 flex h-[46px] w-full items-center justify-center rounded-[14px] bg-fill text-[15px] font-semibold text-label outline-none hover:bg-fill-hover focus-visible:bg-fill-hover"
                  >
                    {t('common.cancel')}
                  </button>
                </Glass>
              </>
            ) : (
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
            )}
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
  // current marks the item that leads to the page already open.
  current?: boolean;
}

// MenuItem runs its action and closes the menu.
export function MenuItem({ icon: Icon, children, onSelect, checked, current }: MenuItemProps) {
  const { close, sheet } = useContext(MenuContext);
  const choice = checked !== undefined;
  return (
    <button
      type="button"
      role={choice ? 'menuitemradio' : 'menuitem'}
      aria-checked={choice ? checked : undefined}
      aria-current={current ? 'page' : undefined}
      tabIndex={-1}
      onClick={() => {
        close();
        onSelect();
      }}
      className={cn(
        'group flex w-full items-center gap-[9px] text-left outline-none',
        sheet ? 'h-[46px] rounded-[14px] px-3.5 text-[15px]' : 'h-[30px] rounded-[9px] px-[9px]',
        'focus-visible:bg-accent-fill focus-visible:text-on-accent hover:bg-accent-fill hover:text-on-accent',
        'text-label',
        current && 'font-semibold',
      )}
    >
      {Icon && (
        <Icon
          className={cn('h-[15px] w-[15px] shrink-0 group-hover:text-on-accent group-focus-visible:text-on-accent', current ? 'text-accent' : 'text-label-2')}
          strokeWidth={1.8}
        />
      )}
      <span className="flex-1 truncate">{children}</span>
      {choice && <Check className={cn('h-[14px] w-[14px] shrink-0', !checked && 'invisible')} strokeWidth={2.2} />}
    </button>
  );
}

export function MenuSeparator() {
  const { sheet } = useContext(MenuContext);
  return <div role="separator" className={cn('my-[5px] h-px bg-separator', sheet ? 'mx-3.5' : 'mx-[9px]')} />;
}

// headingClass styles a heading inside a menu, larger in a sheet.
function headingClass(sheet: boolean) {
  return cn('pb-1 pt-[7px] font-semibold text-label-2', sheet ? 'px-3.5 text-[12px]' : 'px-[9px] text-[11px]');
}

// MenuGroup holds a set of choices under its label, so assistive technology
// reads each set as its own group.
export function MenuGroup({ label, children }: { label: string; children: ReactNode }) {
  const id = useId();
  const { sheet } = useContext(MenuContext);
  return (
    <div role="group" aria-labelledby={id}>
      <div id={id} className={headingClass(sheet)}>{label}</div>
      {children}
    </div>
  );
}

// MenuLabel heads a part of a menu that is not one set of choices, as the
// account's name does in the phone's More sheet.
export function MenuLabel({ children }: { children: ReactNode }) {
  const { sheet } = useContext(MenuContext);
  return <div role="presentation" className={cn('truncate', headingClass(sheet))}>{children}</div>;
}
