import { Plus, Trash2 } from 'lucide-react';
import type { Control, FieldArrayPath, FieldValues, Path, UseFormRegister } from 'react-hook-form';
import { useFieldArray } from 'react-hook-form';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';

interface KeyValueFieldArrayProps<T extends FieldValues> {
  control: Control<T>;
  register: UseFormRegister<T>;
  name: FieldArrayPath<T>;
  keyPlaceholder: string;
  valuePlaceholder: string;
  addLabel: string;
}

export function KeyValueFieldArray<T extends FieldValues>({ control, register, name, keyPlaceholder, valuePlaceholder, addLabel }: KeyValueFieldArrayProps<T>) {
  const { fields, append, remove } = useFieldArray({ control, name });

  return (
    <>
      {fields.map((field, index) => (
        <div key={field.id} className="flex items-start gap-2">
          <div className="flex-1">
            <Input placeholder={keyPlaceholder} {...register(`${name}.${index}.key` as Path<T>)} />
          </div>
          <div className="flex-1">
            <Input placeholder={valuePlaceholder} {...register(`${name}.${index}.value` as Path<T>)} />
          </div>
          <Button type="button" variant="danger" onClick={() => remove(index)} className="px-3">
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      ))}
      <Button type="button" variant="secondary" onClick={() => append({ key: '', value: '' } as never)} className="w-full text-sm">
        <Plus className="mr-2 h-4 w-4" />
        {addLabel}
      </Button>
    </>
  );
}
