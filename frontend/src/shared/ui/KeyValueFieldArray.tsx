import type { Control, FieldArrayPath, FieldValues, Path, UseFormRegister } from 'react-hook-form';
import { useFieldArray } from 'react-hook-form';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { AddRowButton, RemoveRowButton } from '@/shared/ui/RowButtons';
import { useT } from '@/shared/i18n';

interface KeyValueFieldArrayProps<T extends FieldValues> {
  control: Control<T>;
  register: UseFormRegister<T>;
  name: FieldArrayPath<T>;
  // The placeholders also name each row's fields for assistive technology.
  keyPlaceholder: string;
  valuePlaceholder: string;
  addLabel: string;
  // errors holds each row's key message, by row.
  errors?: readonly (string | undefined)[];
}

// KeyValueFieldArray edits a list of key and value rows: a row per pair with
// a quiet remove button, and an add link below.
export function KeyValueFieldArray<T extends FieldValues>({ control, register, name, keyPlaceholder, valuePlaceholder, addLabel, errors }: KeyValueFieldArrayProps<T>) {
  const { fields, append, remove } = useFieldArray({ control, name });
  const t = useT();

  return (
    <>
      {fields.map((field, index) => (
        <div key={field.id} className="flex items-start gap-2">
          <Field error={errors?.[index]} className="flex-1">
            <Input aria-label={keyPlaceholder} placeholder={keyPlaceholder} className="font-mono" autoComplete="off" spellCheck={false} error={!!errors?.[index]} {...register(`${name}.${index}.key` as Path<T>)} />
          </Field>
          <div className="flex-1">
            <Input aria-label={valuePlaceholder} placeholder={valuePlaceholder} className="font-mono" autoComplete="off" spellCheck={false} {...register(`${name}.${index}.value` as Path<T>)} />
          </div>
          <RemoveRowButton label={t('common.delete')} onClick={() => remove(index)} />
        </div>
      ))}
      <AddRowButton label={addLabel} onClick={() => append({ key: '', value: '' } as never)} />
    </>
  );
}
