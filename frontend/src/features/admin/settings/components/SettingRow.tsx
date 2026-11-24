import { useState, type KeyboardEvent } from 'react';
import { useController, useFormState, type Control } from 'react-hook-form';
import { X } from 'lucide-react';

import { Input } from '@/shared/ui/Input';
import { Select } from '@/shared/ui/Select';
import type { TFunction, TranslationKey } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import type { SettingDef } from '../catalogue';
import {
  byteUnitFor,
  byteUnits,
  bytesToUnit,
  coresToMillicores,
  formatBytes,
  millicoresToCores,
  unitToBytes,
  type ByteUnit,
  type SettingName,
  type SystemConfig,
} from '../model';
import type { SettingErrorCode } from '../schemas';

const listPlaceholders: Partial<Record<SettingName, TranslationKey>> = {
  reserved_domain_prefixes: 'admin.settings.placeholder.addPrefix',
  blocked_domain_prefix_patterns: 'admin.settings.placeholder.addPrefixPattern',
  git_allowed_hosts: 'admin.settings.placeholder.addHost',
  telemetry_allowed_exec_commands: 'admin.settings.placeholder.addCommand',
};

/**
 * One runtime setting: its label and description beside its field and unit,
 * or above it on compact content. While its value differs from the saved one
 * the row is marked with a dot, an outlined field and the saved value.
 */
export function SettingRow({ setting, control, compact, t }: {
  setting: SettingDef;
  control: Control<SystemConfig>;
  compact: boolean;
  t: TFunction;
}) {
  const { field, fieldState } = useController({ name: setting.name, control, rules: { deps: setting.affects } });
  const { defaultValues } = useFormState({ control, name: setting.name });
  const error = fieldState.error ? errorText(fieldState.error.message, setting, t) : undefined;
  const changed = fieldState.isDirty;
  const descriptionId = `${setting.name}-description`;
  const wasId = `${setting.name}-was`;
  const shared = {
    id: setting.name,
    describedBy: changed ? `${descriptionId} ${wasId}` : descriptionId,
    invalid: !!error,
    changed,
    t,
  };
  const wide = setting.kind === 'list';
  // Lines under a field beside its label follow the field's right edge.
  const underField = !compact && !wide && 'text-right';

  return (
    <div
      className={cn(
        'grid gap-x-6 gap-y-2.5 border-t border-separator px-1 py-3.5',
        !compact && !wide && 'grid-cols-[minmax(0,1fr)_minmax(10rem,16rem)] items-center',
      )}
    >
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          {changed && <span aria-hidden className="h-[7px] w-[7px] shrink-0 rounded-full bg-accent-fill" />}
          <label htmlFor={setting.name} className="font-semibold text-label">
            {t(`admin.settings.field.${setting.name}.label`)}
          </label>
        </div>
        <p id={descriptionId} className="mt-0.5 text-[12.5px] leading-normal text-label-2">
          {t(`admin.settings.field.${setting.name}.hint`)}
        </p>
      </div>
      <div className={cn('min-w-0', setting.kind === 'toggle' && !compact && 'flex flex-col items-end')}>
        {setting.kind === 'toggle' && (
          <Toggle {...shared} checked={field.value === true} onChange={field.onChange} />
        )}
        {setting.kind === 'list' && (
          <ListInput
            {...shared}
            items={Array.isArray(field.value) ? field.value : []}
            onChange={field.onChange}
            placeholder={t(listPlaceholders[setting.name] ?? 'admin.settings.addListItem')}
          />
        )}
        {setting.kind === 'size' && (
          <Input
            id={setting.name}
            aria-describedby={shared.describedBy}
            aria-invalid={!!error}
            error={!!error}
            className={cn('text-right font-mono tabular-nums', changedOutline(changed, !!error))}
            value={typeof field.value === 'string' ? field.value : ''}
            onChange={(event) => field.onChange(event.target.value.trim())}
            onBlur={field.onBlur}
          />
        )}
        {setting.kind === 'bytes' && (
          <ByteInput {...shared} bytes={numberOrUndefined(field.value)} onChange={field.onChange} />
        )}
        {(setting.kind === 'integer' || setting.kind === 'decimal') && (
          <NumberInput
            {...shared}
            value={numberOrUndefined(field.value)}
            onChange={field.onChange}
            step={setting.kind === 'decimal' ? 'any' : 1}
            suffix={setting.unit ? t(`admin.settings.unit.${setting.unit}`) : undefined}
          />
        )}
        {setting.kind === 'cores' && (
          <NumberInput
            {...shared}
            value={numberOrUndefined(field.value)}
            onChange={field.onChange}
            toShown={millicoresToCores}
            fromShown={coresToMillicores}
            step="any"
            suffix={t('admin.settings.unit.cores')}
          />
        )}
        {changed && (
          <p id={wasId} className={cn('mt-1.5 text-xs font-[550] text-accent', underField)}>
            {t('admin.settings.was', { value: shownValue(defaultValues?.[setting.name], setting, t) })}
          </p>
        )}
        {error && <p role="alert" className={cn('mt-1.5 text-xs leading-snug text-red-text', underField)}>{error}</p>}
      </div>
    </div>
  );
}

/** The outline of a changed field; a refused one keeps its red border instead. */
function changedOutline(changed: boolean, invalid: boolean) {
  return changed && !invalid ? 'border-accent-fill' : undefined;
}

/** A saved value as the row shows it, in the unit its field uses. */
function shownValue(value: unknown, setting: SettingDef, t: TFunction) {
  if (typeof value === 'boolean') return value ? t('admin.settings.enabled') : t('admin.settings.disabled');
  if (Array.isArray(value)) return value.length ? value.join(', ') : t('admin.settings.none');
  if (typeof value === 'string') return value;
  if (typeof value !== 'number') return t('admin.settings.none');
  if (setting.kind === 'bytes') return formatBytes(value);
  if (setting.kind === 'cores') return `${millicoresToCores(value)} ${t('admin.settings.unit.cores')}`;
  return setting.unit ? `${value} ${t(`admin.settings.unit.${setting.unit}`)}` : String(value);
}

function numberOrUndefined(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined;
}

/** The sentence for a refused value, with the setting's range in the unit it is shown in. */
function errorText(code: string | undefined, setting: SettingDef, t: TFunction) {
  const shown = (value: number) => {
    if (setting.kind === 'bytes') return formatBytes(value);
    if (setting.kind === 'cores') return String(millicoresToCores(value));
    return String(value);
  };
  switch (code as SettingErrorCode) {
    case 'integer':
      return t('admin.settings.error.integer');
    case 'size':
      return t('admin.settings.error.size');
    case 'listRequired':
      return t('admin.settings.error.listRequired');
    case 'listItem':
      return t('admin.settings.error.listItem');
    case 'listUnique':
      return t('admin.settings.error.listUnique');
    case 'boostFloor':
      return t('admin.settings.error.boostFloor');
    case 'aboveMemoryQuota':
      return t('admin.settings.error.aboveMemoryQuota');
    case 'aboveCPUQuota':
      return t('admin.settings.error.aboveCPUQuota');
    case 'aboveDiskQuota':
      return t('admin.settings.error.aboveDiskQuota');
    case 'range': {
      const min = shown(setting.min ?? 0);
      if (setting.max === undefined) return t('admin.settings.error.rangeFrom', { min });
      const max = shown(setting.max);
      return t(setting.aboveMin ? 'admin.settings.error.rangeAbove' : 'admin.settings.error.rangeBetween', { min, max });
    }
    default:
      return t('admin.settings.error.required');
  }
}

interface ControlProps {
  id: string;
  describedBy: string;
  invalid: boolean;
  /** The value differs from the saved one, so the field is outlined. */
  changed: boolean;
  t: TFunction;
}

/**
 * A number field that keeps what is being typed. The form holds a number, or
 * nothing while the field is empty or unreadable; the typed text is shown only
 * while the form still holds the value it produced, so any other change (a
 * reset, a discard) shows the formatted value again.
 */
function NumberInput({ id, describedBy, invalid, changed, value, onChange, toShown, fromShown, step, suffix, className }: Omit<ControlProps, 't'> & {
  value: number | undefined;
  onChange: (value: number | undefined) => void;
  toShown?: (value: number) => number;
  fromShown?: (shown: number) => number;
  step: number | 'any';
  suffix?: string;
  className?: string;
}) {
  const [draft, setDraft] = useState<{ text: string; value: number | undefined } | null>(null);
  const formatted = value === undefined ? '' : String(toShown ? toShown(value) : value);
  const text = draft && draft.value === value ? draft.text : formatted;

  // One box holds the number and its unit a small gap apart, so the number
  // ends just before the unit whatever the unit's length.
  return (
    <div
      className={cn(
        'flex items-center gap-1.5 rounded-xl border bg-fill pr-4 text-sm transition-all',
        'focus-within:ring-2 focus-within:ring-accent-fill',
        invalid ? 'border-red focus-within:ring-red' : changed ? 'border-accent-fill' : 'border-transparent',
        className,
      )}
    >
      <input
        id={id}
        type="number"
        inputMode="decimal"
        step={step}
        aria-describedby={describedBy}
        aria-invalid={invalid}
        className={cn(
          'min-w-0 flex-1 bg-transparent py-2.5 pl-4 text-right tabular-nums text-label placeholder:text-label-3 focus-visible:outline-none',
          '[appearance:textfield] [&::-webkit-inner-spin-button]:appearance-none [&::-webkit-outer-spin-button]:appearance-none',
        )}
        value={text}
        onChange={(event) => {
          const raw = event.target.value;
          const parsed = raw.trim() === '' ? Number.NaN : Number(raw);
          const next = Number.isFinite(parsed) ? (fromShown ? fromShown(parsed) : parsed) : undefined;
          setDraft({ text: raw, value: next });
          onChange(next);
        }}
        onBlur={() => setDraft((current) => (current && current.value === undefined ? current : null))}
      />
      {suffix && <span aria-hidden className="shrink-0 text-label-2">{suffix}</span>}
    </div>
  );
}

function ByteInput({ id, describedBy, invalid, changed, bytes, onChange, t }: ControlProps & {
  bytes: number | undefined;
  onChange: (value: number | undefined) => void;
}) {
  // Until the administrator picks a unit, the unit follows the value.
  const [chosenUnit, setChosenUnit] = useState<ByteUnit | null>(null);
  const unit = chosenUnit ?? byteUnitFor(bytes ?? 0);

  return (
    <div className="flex gap-1.5">
      <NumberInput
        // A new unit reformats the number, so what was typed is dropped.
        key={unit}
        id={id}
        describedBy={describedBy}
        invalid={invalid}
        changed={changed}
        value={bytes}
        onChange={onChange}
        toShown={(value) => bytesToUnit(value, unit)}
        fromShown={(shown) => unitToBytes(shown, unit)}
        step="any"
        className="min-w-0 flex-1"
      />
      <Select
        value={unit}
        onChange={(event) => setChosenUnit(event.target.value as ByteUnit)}
        className="w-[4.75rem] shrink-0 px-3 text-label-2"
        aria-label={t('admin.settings.byteUnit')}
      >
        {byteUnits.map((item) => (
          <option key={item.label} value={item.label}>{item.label}</option>
        ))}
      </Select>
    </div>
  );
}

function Toggle({ id, describedBy, changed, checked, onChange, t }: ControlProps & {
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return (
    <button
      id={id}
      type="button"
      role="switch"
      aria-checked={checked}
      aria-describedby={describedBy}
      onClick={() => onChange(!checked)}
      className="inline-flex items-center gap-2.5 rounded-full text-label-2"
    >
      <span>{checked ? t('admin.settings.enabled') : t('admin.settings.disabled')}</span>
      <span
        aria-hidden
        className={cn(
          'relative h-[26px] w-[44px] shrink-0 rounded-full transition-colors',
          checked ? 'bg-accent-fill' : 'bg-fill-hover',
          changed && 'outline outline-[1.5px] outline-offset-2 outline-accent-fill',
        )}
      >
        <span
          className={cn(
            // left-0: a button centres its text, which would move the knob's
            // start to the middle of the track.
            'absolute left-0 top-[3px] h-5 w-5 rounded-full bg-on-accent shadow-[var(--lift-shadow)] transition-transform',
            checked ? 'translate-x-[21px]' : 'translate-x-[3px]',
          )}
        />
      </span>
    </button>
  );
}

/** A list edited as chips: each entry removable, new ones typed in after them and added with Enter or a comma. */
function ListInput({ id, describedBy, invalid, changed, items, onChange, placeholder, t }: ControlProps & {
  items: string[];
  onChange: (items: string[]) => void;
  placeholder: string;
}) {
  const [draft, setDraft] = useState('');

  const add = (raw: string) => {
    const values = raw.split(/[\n,]+/).map((item) => item.trim()).filter(Boolean);
    if (!values.length) return;
    onChange([...items, ...values.filter((item, index) => !items.includes(item) && values.indexOf(item) === index)]);
    setDraft('');
  };
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault();
      add(draft);
    } else if (event.key === 'Backspace' && draft === '' && items.length) {
      onChange(items.slice(0, -1));
    }
  };

  return (
    <div
      className={cn(
        'flex min-h-10 flex-wrap items-center gap-1.5 rounded-xl border bg-fill p-1.5',
        'has-[input:focus-visible]:ring-2 has-[input:focus-visible]:ring-accent-fill',
        invalid ? 'border-red' : changed ? 'border-accent-fill' : 'border-transparent',
      )}
    >
      {items.map((item) => (
        <span
          key={item}
          className="inline-flex h-7 max-w-full items-center gap-1 rounded-full bg-lift pl-2.5 pr-1 font-mono text-xs text-label shadow-[var(--lift-shadow)]"
        >
          <span className="truncate">{item}</span>
          <button
            type="button"
            onClick={() => onChange(items.filter((current) => current !== item))}
            className="grid h-5 w-5 shrink-0 place-items-center rounded-full text-label-2 hover:bg-fill-hover hover:text-label"
            title={t('admin.settings.removeListItem', { item })}
            aria-label={t('admin.settings.removeListItem', { item })}
          >
            <X className="h-3 w-3" strokeWidth={2.2} />
          </button>
        </span>
      ))}
      <input
        id={id}
        aria-describedby={describedBy}
        aria-invalid={invalid}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={onKeyDown}
        onBlur={() => add(draft)}
        onPaste={(event) => {
          const pasted = event.clipboardData.getData('text');
          if (pasted.includes('\n') || pasted.includes(',')) {
            event.preventDefault();
            add(pasted);
          }
        }}
        placeholder={items.length ? placeholder : `${t('admin.settings.emptyList')} · ${placeholder}`}
        className="h-7 min-w-[8rem] flex-1 bg-transparent px-1.5 text-label placeholder:text-label-3 focus-visible:outline-none"
      />
    </div>
  );
}
