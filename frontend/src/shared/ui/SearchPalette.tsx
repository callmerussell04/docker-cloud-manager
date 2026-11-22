import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import { CornerDownLeft, Search, type LucideIcon } from 'lucide-react';
import { cn } from '@/shared/lib/utils';
import { Glass } from './Glass';
import { Kbd } from './Kbd';
import { moveActive, visibleGroups, type PaletteEntry, type PaletteGroup } from './searchPalette';

export interface PaletteItem extends PaletteEntry {
  icon: LucideIcon;
  // meta follows the label in a quieter monospace, as a container's image.
  meta?: string;
  onSelect: () => void;
}

interface SearchPaletteProps {
  open: boolean;
  onClose: () => void;
  groups: PaletteGroup<PaletteItem>[];
  // label names the dialog; placeholder fills the empty field; emptyText is
  // shown when nothing matches.
  label: string;
  placeholder: string;
  emptyText: string;
}

// SearchPalette is a dense glass dialog that finds what its groups hold as
// the user types. It renders at the end of the document, outside whatever
// glass opened it, so its blur sees the page. Focus stays in the field: the
// arrow keys move the active row, Enter chooses it, and Escape or a click
// outside closes the dialog. The field shows no focus ring: it is the
// dialog's only stop, and its caret and the active row show where focus is. Focus returns to where it was when it closes.
export function SearchPalette({ open, ...props }: SearchPaletteProps) {
  if (!open) return null;
  return <PaletteDialog {...props} />;
}

function PaletteDialog({ onClose, groups, label, placeholder, emptyText }: Omit<SearchPaletteProps, 'open'>) {
  const id = useId();
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const listRef = useRef<HTMLDivElement>(null);

  const shown = useMemo(() => visibleGroups(groups, query), [groups, query]);
  const flat = useMemo(() => shown.flatMap((group) => group.items), [shown]);
  // Containers arrive after the palette opens, so the active row may point
  // past the end for a moment.
  const current = Math.min(active, Math.max(flat.length - 1, 0));
  const optionId = (index: number) => `${id}-option-${index}`;

  // Focus moves to the field when the palette opens and returns to where it
  // was when it closes, however it closes.
  const inputRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    inputRef.current?.focus();
    return () => {
      if (previous?.isConnected) previous.focus();
    };
  }, []);

  useLayoutEffect(() => {
    listRef.current?.querySelector(`[id="${optionId(current)}"]`)?.scrollIntoView({ block: 'nearest' });
    // optionId depends only on id, which never changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [current]);

  const choose = (item: PaletteItem | undefined) => {
    if (!item) return;
    onClose();
    item.onSelect();
  };

  const onKeyDown = (event: ReactKeyboardEvent<HTMLInputElement>) => {
    switch (event.key) {
      case 'ArrowDown':
      case 'ArrowUp':
        event.preventDefault();
        setActive(moveActive(current, event.key === 'ArrowDown' ? 1 : -1, flat.length));
        break;
      case 'Home':
      case 'End':
        // Home and End keep moving the caret while there is text.
        if (query !== '') break;
        event.preventDefault();
        setActive(event.key === 'Home' ? 0 : Math.max(flat.length - 1, 0));
        break;
      case 'Enter':
        event.preventDefault();
        choose(flat[current]);
        break;
      case 'Escape':
        // Escape closes only the palette, not a dialog beneath it.
        event.preventDefault();
        event.stopPropagation();
        onClose();
        break;
      case 'Tab':
        // The field is the dialog's only stop, so Tab stays in it.
        event.preventDefault();
        break;
    }
  };

  return createPortal(
    <div className="fixed inset-0 z-[110]">
      <div aria-hidden className="sheet-scrim absolute inset-0 bg-scrim" onClick={onClose} />
      <Glass
        material="dense"
        role="dialog"
        aria-modal="true"
        aria-label={label}
        // Keys are handled in the field, so a click anywhere in the dialog but
        // the field leaves focus there.
        onMouseDown={(event) => {
          if (event.target !== inputRef.current) event.preventDefault();
        }}
        className="menu-pop absolute left-1/2 top-[12vh] w-[min(620px,calc(100%-32px))] -translate-x-1/2 overflow-hidden rounded-[22px]"
      >
        <div className="flex h-[54px] items-center gap-[11px] border-b border-separator px-[18px] text-label-2">
          <Search className="h-[18px] w-[18px] shrink-0" strokeWidth={1.8} />
          <input
            ref={inputRef}
            role="combobox"
            aria-expanded="true"
            aria-controls={`${id}-list`}
            aria-autocomplete="list"
            aria-activedescendant={flat.length > 0 ? optionId(current) : undefined}
            aria-label={label}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setActive(0);
            }}
            onKeyDown={onKeyDown}
            placeholder={placeholder}
            spellCheck={false}
            autoComplete="off"
            className="min-w-0 flex-1 bg-transparent text-[17px] text-label outline-none placeholder:text-label-3 focus-visible:outline-none"
          />
          <Kbd>esc</Kbd>
        </div>
        <div ref={listRef} id={`${id}-list`} role="listbox" aria-label={label} className="max-h-[min(46vh,420px)] overflow-y-auto p-1.5">
          {shown.map((group) => (
            <div key={group.id} role="group" aria-labelledby={`${id}-${group.id}`}>
              <div id={`${id}-${group.id}`} role="presentation" className="px-[9px] pb-1 pt-[7px] text-[11px] font-semibold text-label-2">
                {group.label}
              </div>
              {group.items.map((item) => {
                const index = flat.indexOf(item);
                const selected = index === current;
                return (
                  <div
                    key={item.id}
                    id={optionId(index)}
                    role="option"
                    aria-selected={selected}
                    // The pointer moves the active row only when it moves, so
                    // a row under a resting pointer does not steal the keys'.
                    onMouseMove={() => {
                      if (!selected) setActive(index);
                    }}
                    onClick={() => choose(item)}
                    className={cn(
                      'flex h-9 cursor-default items-center gap-[9px] rounded-[9px] px-[9px]',
                      selected ? 'bg-accent-fill text-on-accent' : 'text-label',
                    )}
                  >
                    <item.icon className={cn('h-[15px] w-[15px] shrink-0', selected ? 'text-on-accent' : 'text-label-2')} strokeWidth={1.8} />
                    <span className="min-w-0 truncate">{item.label}</span>
                    <span className={cn('min-w-0 flex-1 truncate font-mono text-[11.5px]', selected ? 'text-on-accent' : 'text-label-3')}>{item.meta}</span>
                    {selected && <CornerDownLeft aria-hidden className="h-[14px] w-[14px] shrink-0" strokeWidth={1.8} />}
                  </div>
                );
              })}
            </div>
          ))}
          {flat.length === 0 && (
            <div role="status" className="px-5 py-5 text-center text-label-2">
              {emptyText}
            </div>
          )}
        </div>
      </Glass>
    </div>,
    document.body,
  );
}
