import { useState, type KeyboardEvent } from 'react';
import type { UseFormRegister, UseFormSetValue, UseFormWatch } from 'react-hook-form';
import { Plus, X } from 'lucide-react';

import { Button } from '@/shared/ui/Button';
import { Input } from '@/shared/ui/Input';
import { Label } from '@/shared/ui/Label';
import { Select } from '@/shared/ui/Select';
import type { TFunction } from '@/shared/i18n';
import { cn } from '@/shared/lib/utils';
import type { SystemConfigForm } from '../schemas';
import {
  byteUnits,
  formatNumberInput,
  getErrorMessage,
  numericInputValue,
  preferredByteUnit,
  type ByteUnit,
  type ConfigFieldConfig,
} from './configFields';
import { InfoHint } from './InfoHint';

/** Renders one runtime setting with the input its type calls for. */
export function ConfigField({
  field,
  register,
  setValue,
  watch,
  error,
  t,
}: {
  field: ConfigFieldConfig;
  register: UseFormRegister<SystemConfigForm>;
  setValue: UseFormSetValue<SystemConfigForm>;
  watch: UseFormWatch<SystemConfigForm>;
  error: unknown;
  t: TFunction;
}) {
  if (field.type === 'boolean') {
    return <SwitchField field={field} setValue={setValue} watch={watch} error={error} t={t} />;
  }

  if (field.type === 'array') {
    return <ListField field={field} setValue={setValue} watch={watch} error={error} t={t} />;
  }

  if (field.type === 'bytes') {
    return <ByteField field={field} setValue={setValue} watch={watch} error={error} t={t} />;
  }

  const isNumber = field.type === 'number' || field.type === 'float';
  const errorMessage = getErrorMessage(error);

  return (
    <div className="space-y-2">
      <FieldLabel field={field} t={t} />
      <div className="relative">
        <Input
          id={field.name}
          type={isNumber ? 'number' : 'text'}
          step={field.type === 'float' ? '0.1' : undefined}
          placeholder={field.placeholderKey ? t(field.placeholderKey) : undefined}
          error={!!error}
          className={field.unitKey ? 'pr-24' : undefined}
          {...register(field.name, isNumber ? { setValueAs: numericInputValue } : undefined)}
        />
        {field.unitKey && (
          <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-xs font-medium text-slate-500 dark:text-slate-400">
            {t(field.unitKey)}
          </span>
        )}
      </div>
      <FieldError message={errorMessage} />
    </div>
  );
}

function FieldLabel({ field, t }: { field: ConfigFieldConfig; t: TFunction }) {
  return (
    <div className="flex items-center gap-2">
      <Label htmlFor={field.name}>{t(field.labelKey)}</Label>
      <InfoHint text={t(field.hintKey)} label={t(field.labelKey)} t={t} />
    </div>
  );
}

function FieldError({ message }: { message?: string }) {
  if (!message) return null;
  return <p className="text-xs font-medium text-red-600 dark:text-red-400">{message}</p>;
}

function SwitchField({
  field,
  setValue,
  watch,
  error,
  t,
}: {
  field: ConfigFieldConfig;
  setValue: UseFormSetValue<SystemConfigForm>;
  watch: UseFormWatch<SystemConfigForm>;
  error: unknown;
  t: TFunction;
}) {
  const checked = Boolean(watch(field.name));
  const errorMessage = getErrorMessage(error);

  return (
    <div className="space-y-2">
      <FieldLabel field={field} t={t} />
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => setValue(field.name, !checked as never, { shouldDirty: true, shouldValidate: true })}
        className={cn(
          'flex w-full items-center justify-between rounded-xl border px-4 py-3 text-left text-sm transition-colors focus:outline-none focus:ring-2 focus:ring-indigo-500',
          checked
            ? 'border-indigo-300 bg-indigo-50 text-indigo-900 dark:border-indigo-800 dark:bg-indigo-950/40 dark:text-indigo-100'
            : 'border-white/40 bg-white/40 text-slate-700 dark:border-slate-700/50 dark:bg-slate-900/40 dark:text-slate-300'
        )}
      >
        <span className="font-medium">{checked ? t('admin.settings.enabled') : t('admin.settings.disabled')}</span>
        <span
          className={cn(
            'relative h-6 w-11 rounded-full transition-colors',
            checked ? 'bg-indigo-600' : 'bg-slate-300 dark:bg-slate-700'
          )}
        >
          <span
            className={cn(
              'absolute top-1 h-4 w-4 rounded-full bg-white shadow transition-transform',
              checked ? 'translate-x-6' : 'translate-x-1'
            )}
          />
        </span>
      </button>
      <FieldError message={errorMessage} />
    </div>
  );
}

function ByteField({
  field,
  setValue,
  watch,
  error,
  t,
}: {
  field: ConfigFieldConfig;
  setValue: UseFormSetValue<SystemConfigForm>;
  watch: UseFormWatch<SystemConfigForm>;
  error: unknown;
  t: TFunction;
}) {
  const watchedValue = watch(field.name);
  const bytes = typeof watchedValue === 'number' && Number.isFinite(watchedValue) ? watchedValue : 0;
  // Until the administrator picks a unit or types, the unit follows the value.
  const [chosenUnit, setChosenUnit] = useState<ByteUnit | null>(null);
  // What the administrator is typing, and the form value it produced. It is
  // shown only while the form still holds that value, so an empty field stays
  // empty while it is edited and any other change shows the formatted value.
  const [draft, setDraft] = useState<{ text: string; value: number | undefined } | null>(null);
  const unit = chosenUnit ?? preferredByteUnit(bytes);
  const currentUnit = byteUnits.find((item) => item.label === unit) ?? byteUnits[0];
  const displayValue = bytes / currentUnit.multiplier;
  const inputValue = draft && draft.value === watchedValue ? draft.text : formatNumberInput(displayValue);
  const errorMessage = getErrorMessage(error);

  return (
    <div className="space-y-2">
      <FieldLabel field={field} t={t} />
      <div className="flex gap-2">
        <Input
          id={field.name}
          type="number"
          step={unit === 'Bytes' ? 1 : 0.01}
          value={inputValue}
          error={!!error}
          onChange={(event) => {
            const rawValue = event.target.value;
            const value = rawValue.trim() === '' ? Number.NaN : Number(rawValue);
            const nextBytes = Number.isFinite(value) ? Math.round(value * currentUnit.multiplier) : undefined;
            setChosenUnit(unit);
            setDraft({ text: rawValue, value: nextBytes });
            setValue(field.name, nextBytes as never, { shouldDirty: true, shouldValidate: true });
          }}
          // A cleared or unreadable field stays as typed: the form holds no value to show.
          onBlur={() => setDraft((current) => (current && current.value === undefined ? current : null))}
        />
        <Select
          value={unit}
          onChange={(event) => {
            setChosenUnit(event.target.value as ByteUnit);
            setDraft(null);
          }}
          className="w-28 shrink-0"
          aria-label={t('admin.settings.byteUnit')}
        >
          {byteUnits.map((item) => (
            <option key={item.label} value={item.label}>{item.label}</option>
          ))}
        </Select>
      </div>
      <p className="text-xs text-slate-500 dark:text-slate-400">
        {t('admin.settings.rawBytes', { value: bytes })}
      </p>
      <FieldError message={errorMessage} />
    </div>
  );
}

function ListField({
  field,
  setValue,
  watch,
  error,
  t,
}: {
  field: ConfigFieldConfig;
  setValue: UseFormSetValue<SystemConfigForm>;
  watch: UseFormWatch<SystemConfigForm>;
  error: unknown;
  t: TFunction;
}) {
  const items = Array.isArray(watch(field.name)) ? watch(field.name) as string[] : [];
  const [draft, setDraft] = useState('');
  const errorMessage = getErrorMessage(error);

  const updateItems = (nextItems: string[]) => {
    setValue(field.name, nextItems as never, { shouldDirty: true, shouldValidate: true });
  };
  const addDraft = (raw: string) => {
    const values = raw
      .split(/[\n,]+/)
      .map((item) => item.trim())
      .filter(Boolean);

    if (!values.length) return;

    updateItems([...items, ...values.filter((item) => !items.includes(item))]);
    setDraft('');
  };
  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault();
      addDraft(draft);
    }
  };

  return (
    <div className="space-y-2">
      <FieldLabel field={field} t={t} />
      <div className="rounded-xl border border-white/40 bg-white/40 p-3 dark:border-slate-700/50 dark:bg-slate-900/40">
        <div className="flex min-h-10 flex-wrap gap-2">
          {items.length ? items.map((item) => (
            <span key={item} className="inline-flex max-w-full items-center gap-1 rounded-lg bg-slate-100 px-2.5 py-1 text-sm text-slate-700 dark:bg-slate-800 dark:text-slate-200">
              <span className="truncate">{item}</span>
              <button
                type="button"
                onClick={() => updateItems(items.filter((current) => current !== item))}
                className="rounded p-0.5 text-slate-500 hover:bg-slate-200 hover:text-red-600 dark:hover:bg-slate-700"
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
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={onKeyDown}
            onPaste={(event) => {
              const pasted = event.clipboardData.getData('text');
              if (pasted.includes('\n') || pasted.includes(',')) {
                event.preventDefault();
                addDraft(pasted);
              }
            }}
            placeholder={field.placeholderKey ? t(field.placeholderKey) : t('admin.settings.addListItem')}
            error={!!error}
          />
          <Button title={t('admin.settings.addListItem')} aria-label={t('admin.settings.addListItem')} type="button" variant="secondary" onClick={() => addDraft(draft)} className="shrink-0 px-3">
            <Plus className="h-4 w-4" />
          </Button>
        </div>
      </div>
      <FieldError message={errorMessage} />
    </div>
  );
}
