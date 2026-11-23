import { forwardRef, useId, useState, type KeyboardEvent } from 'react';
import { Disc, Search } from 'lucide-react';
import { useT } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import { imageSuggestions, popularImages } from '../createFlow';

interface ImageFieldProps {
  id: string;
  value: string;
  onChange: (image: string) => void;
  // ownImages are the tags of the user's images, offered first.
  ownImages: readonly string[];
  'aria-describedby'?: string;
  'aria-invalid'?: boolean;
}

// ImageField is one combobox for an image: it offers the user's images and
// popular Docker Hub images matching what is typed, and takes any typed tag
// as it is. Arrow keys move through the options, Enter chooses one, and
// Escape closes the list without closing the dialog.
export const ImageField = forwardRef<HTMLInputElement, ImageFieldProps>(function ImageField(
  { id, value, onChange, ownImages, ...described },
  ref,
) {
  const t = useT();
  const listId = useId();
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const groups = imageSuggestions(value, ownImages, popularImages);
  const options = [...groups.own, ...groups.popular];
  const expanded = open && options.length > 0;

  const choose = (image: string) => {
    onChange(image);
    setOpen(false);
    setActive(-1);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      if (!expanded) {
        setOpen(true);
        return;
      }
      // From no active option, down starts at the first and up at the last;
      // past either end it wraps.
      const step = event.key === 'ArrowDown' ? 1 : -1;
      setActive((current) => (current < 0 ? (step > 0 ? 0 : options.length - 1) : (current + step + options.length) % options.length));
      return;
    }
    if (event.key === 'Enter' && expanded && active >= 0 && active < options.length) {
      event.preventDefault();
      choose(options[active]);
      return;
    }
    if (event.key === 'Escape' && expanded) {
      event.preventDefault();
      setOpen(false);
      setActive(-1);
    }
  };

  const optionId = (index: number) => `${listId}-${index}`;
  const renderGroup = (label: string, images: string[], offset: number) => images.length > 0 && (
    <li role="presentation">
      <div className="px-3 pb-1 pt-2 text-xs font-semibold text-label-3">{label}</div>
      <ul role="group" aria-label={label}>
        {images.map((image, index) => (
          <li
            key={image}
            id={optionId(offset + index)}
            role="option"
            aria-selected={active === offset + index}
            // A mousedown would take focus off the field and close the list
            // before the click lands.
            onMouseDown={(event) => event.preventDefault()}
            title={image}
            onClick={() => choose(image)}
            className={cn(
              'flex cursor-pointer items-center gap-2 rounded-lg px-3 py-1.5',
              active === offset + index ? 'bg-fill-hover text-label' : 'text-label hover:bg-fill',
            )}
          >
            <Disc aria-hidden className="h-3.5 w-3.5 shrink-0 text-label-2" />
            <span className="min-w-0 truncate font-mono text-xs">{image}</span>
          </li>
        ))}
      </ul>
    </li>
  );

  return (
    <div className="relative">
      <div className="relative">
        <Search aria-hidden className="pointer-events-none absolute left-3.5 top-1/2 h-4 w-4 -translate-y-1/2 text-label-3" />
        <input
          ref={ref}
          id={id}
          role="combobox"
          aria-expanded={expanded}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={expanded && active >= 0 && active < options.length ? optionId(active) : undefined}
          autoComplete="off"
          spellCheck={false}
          value={value}
          placeholder={t('containers.new.imagePlaceholder')}
          onChange={(event) => {
            onChange(event.target.value);
            setOpen(true);
            setActive(-1);
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => setOpen(false)}
          onKeyDown={onKeyDown}
          className={cn(
            'flex w-full rounded-xl border border-transparent bg-fill py-2.5 pl-10 pr-4 font-mono text-sm text-label transition-all placeholder:font-sans placeholder:text-label-3 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-fill',
            described['aria-invalid'] && 'border-red focus-visible:ring-red',
          )}
          {...described}
        />
      </div>
      {expanded && (
        <ul id={listId} role="listbox" aria-label={t('containers.new.imageOptions')} className="mt-1.5 max-h-56 overflow-y-auto rounded-xl border border-separator p-1">
          {renderGroup(t('containers.new.ownImages'), groups.own, 0)}
          {renderGroup(t('containers.new.popularImages'), groups.popular, groups.own.length)}
        </ul>
      )}
    </div>
  );
});
