import { useState, type KeyboardEvent } from 'react';
import { useController, type Control } from 'react-hook-form';
import { Plus, X } from 'lucide-react';

import { Button } from '@/shared/ui/Button';
import { Input } from '@/shared/ui/Input';
import { Label } from '@/shared/ui/Label';
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

/** One runtime setting: its name and description beside the control its kind calls for. */
export function SettingRow({ setting, control, t }: {
  setting: SettingDef;
  control: Control<SystemConfig>;
  t: TFunction;
}) {
  const { field, fieldState } = useController({ name: setting.name, control, rules: { deps: setting.affects } });
  const error = fieldState.error ? errorText(fieldState.error.message, setting, t) : undefined;
  const descriptionId = `${setting.name}-description`;
  const shared = { id: setting.name, describedBy: descriptionId, invalid: !!error, t };

  return (
    <div className="grid gap-3 py-5 md:grid-cols-[minmax(0,1fr)_minmax(0,20rem)] md:gap-8">
      <div className="min-w-0 space-y-1">
        <div className="flex items-center gap-2">
          <Label htmlFor={setting.name}>{t(`admin.settings.field.${setting.name}.label`)}</Label>
          {fieldState.isDirty && <span aria-hidden className="h-2 w-2 shrink-0 rounded-full bg-accent-fill" />}
        </div>
        <p id={descriptionId} className="text-sm leading-relaxed text-slate-500 dark:text-slate-400">
          {t(`admin.settings.field.${setting.name}.hint`)}
        </p>
      </div>
      <div className="min-w-0 space-y-2">
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
            aria-describedby={descriptionId}
            aria-invalid={!!error}
            error={!!error}
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
        {error && <p role="alert" className="text-xs leading-snug text-red-text">{error}</p>}
      </div>
    </div>
  );
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
  t: TFunction;
}

/**
 * A number field that keeps what is being typed. The form holds a number, or
 * nothing while the field is empty or unreadable; the typed text is shown only
 * while the form still holds the value it produced, so any other change (a
 * reset, a discard) shows the formatted value again.
 */
function NumberInput({ id, describedBy, invalid, value, onChange, toShown, fromShown, step, suffix, className }: Omit<ControlProps, 't'> & {
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

  return (
    <div className={cn('relative', className)}>
      <Input
        id={id}
        type="number"
        inputMode="decimal"
        step={step}
        aria-describedby={describedBy}
        aria-invalid={invalid}
        error={invalid}
        className={suffix ? 'pr-20' : undefined}
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
      {suffix && (
        <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs font-medium text-slate-500 dark:text-slate-400">
          {suffix}
        </span>
      )}
    </div>
  );
}

function ByteInput({ id, describedBy, invalid, bytes, onChange, t }: ControlProps & {
  bytes: number | undefined;
  onChange: (value: number | undefined) => void;
}) {
  // Until the administrator picks a unit, the unit follows the value.
  const [chosenUnit, setChosenUnit] = useState<ByteUnit | null>(null);
  const unit = chosenUnit ?? byteUnitFor(bytes ?? 0);

  return (
    <div className="flex gap-2">
      <NumberInput
        // A new unit reformats the number, so what was typed is dropped.
        key={unit}
        id={id}
        describedBy={describedBy}
        invalid={invalid}
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
        className="w-24 shrink-0"
        aria-label={t('admin.settings.byteUnit')}
      >
        {byteUnits.map((item) => (
          <option key={item.label} value={item.label}>{item.label}</option>
        ))}
      </Select>
    </div>
  );
}

function Toggle({ id, describedBy, checked, onChange, t }: ControlProps & {
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
      className={cn(
        'flex w-full items-center justify-between rounded-xl border px-4 py-2.5 text-left text-sm transition-colors focus:outline-none focus:ring-2 focus:ring-accent-fill',
        checked
          ? 'border-separator bg-fill text-accent'
          : 'border-white/40 bg-white/40 text-slate-700 dark:border-slate-700/50 dark:bg-slate-900/40 dark:text-slate-300',
      )}
    >
      <span className="font-medium">{checked ? t('admin.settings.enabled') : t('admin.settings.disabled')}</span>
      <span className={cn('relative h-6 w-11 rounded-full transition-colors', checked ? 'bg-accent-fill' : 'bg-fill-hover')}>
        <span className={cn('absolute top-1 h-4 w-4 rounded-full bg-white shadow transition-transform', checked ? 'translate-x-6' : 'translate-x-1')} />
      </span>
    </button>
  );
}

function ListInput({ id, describedBy, invalid, items, onChange, placeholder, t }: ControlProps & {
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
    }
  };

  return (
    <div className={cn(
      'rounded-xl border bg-white/40 p-3 dark:bg-slate-900/40',
      invalid ? 'border-red-500 dark:border-red-500/50' : 'border-white/40 dark:border-slate-700/50',
    )}>
      <div className="flex min-h-8 flex-wrap gap-2">
        {items.length ? items.map((item) => (
          <span key={item} className="inline-flex max-w-full items-center gap-1 rounded-lg bg-slate-100 px-2.5 py-1 text-sm text-slate-700 dark:bg-slate-800 dark:text-slate-200">
            <span className="truncate">{item}</span>
            <button
              type="button"
              onClick={() => onChange(items.filter((current) => current !== item))}
              className="rounded p-0.5 text-slate-500 hover:bg-slate-200 hover:text-red-600 dark:hover:bg-slate-700"
              title={t('admin.settings.removeListItem', { item })}
              aria-label={t('admin.settings.removeListItem', { item })}
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </span>
        )) : (
          <span className="py-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.settings.emptyList')}</span>
        )}
      </div>
      <div className="mt-3 flex gap-2">
        <Input
          id={id}
          aria-describedby={describedBy}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={onKeyDown}
          onPaste={(event) => {
            const pasted = event.clipboardData.getData('text');
            if (pasted.includes('\n') || pasted.includes(',')) {
              event.preventDefault();
              add(pasted);
            }
          }}
          placeholder={placeholder}
        />
        <Button
          type="button"
          variant="secondary"
          onClick={() => add(draft)}
          title={t('admin.settings.addListItem')}
          aria-label={t('admin.settings.addListItem')}
          className="shrink-0 px-3"
        >
          <Plus className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}
