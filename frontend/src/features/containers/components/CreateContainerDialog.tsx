import { useEffect, useState, type ReactNode } from 'react';
import { useFieldArray, useForm, useWatch, type Path } from 'react-hook-form';
import { Globe, Lock } from 'lucide-react';

import { useT } from '@/shared/i18n';
import { BASE_DOMAIN } from '@/shared/lib/config';
import { formatBytes } from '@/shared/lib/utils';
import { ChoiceCards } from '@/shared/ui/ChoiceCards';
import { Field } from '@/shared/ui/Field';
import { Input } from '@/shared/ui/Input';
import { KeyValueFieldArray } from '@/shared/ui/KeyValueFieldArray';
import { Meter } from '@/shared/ui/Meter';
import { AddRowButton, RemoveRowButton } from '@/shared/ui/RowButtons';
import { Select } from '@/shared/ui/Select';
import { StepDialog } from '@/shared/ui/StepDialog';
import { isImageAvailable } from '@/features/images/model';
import { useImages } from '@/features/images/queries';
import { isVolumeMountable } from '@/features/volumes/model';
import { useVolumes } from '@/features/volumes/queries';
import {
  addressPreview,
  createSteps,
  memoryQuotaFit,
  nameFromImage,
  stepIssues,
  subdomainFrom,
  typedFigure,
  MiB,
  type CreateStep,
  type MemoryQuota,
} from '../createFlow';
import { formatCores, type CreateContainerDTO } from '../model';
import { useCreateContainer } from '../queries';
import { createContainerSchema, type CreateContainerForm, type CreateContainerValues } from '../schemas';
import { ImageField } from './ImageField';

interface CreateContainerDialogProps {
  isOpen: boolean;
  onClose: () => void;
  // memory is the user's memory quota and what they already reserve, for the
  // quota bar; undefined while it is not known.
  memory?: MemoryQuota;
  // initialImage is the image the dialog starts with, as an image's Run a
  // container gives it; the name follows it as it would a chosen image.
  initialImage?: string;
}

// CreateContainerDialog creates a container in four steps: Image, Publishing,
// Settings (resources, environment variables, volume mounts) and Review. Its
// state starts over each time it opens.
export function CreateContainerDialog({ isOpen, onClose, memory, initialImage }: CreateContainerDialogProps) {
  if (!isOpen) return null;
  return <CreateContainerFlow onClose={onClose} memory={memory} initialImage={initialImage} />;
}

const emptyForm: CreateContainerForm = {
  name: '',
  image_tag: '',
  publish: false,
  domain_prefix: '',
  internal_port: '',
  memory_reservation_mb: '',
  cpu_reservation_cores: '',
  env_vars: [],
  volume_mounts: [],
};

function toDTO(values: CreateContainerValues): CreateContainerDTO {
  const dto: CreateContainerDTO = {
    name: values.name,
    image_tag: values.image_tag,
    internal_port: values.internal_port,
    domain_prefix: values.domain_prefix,
    memory_reservation_mb: values.memory_reservation_mb,
    cpu_reservation_millicores: values.cpu_reservation_millicores,
  };
  if (values.env_vars?.length) {
    dto.env_vars = Object.fromEntries(values.env_vars.map(({ key, value }) => [key, value]));
  }
  if (values.volume_mounts?.length) dto.volume_mounts = values.volume_mounts;
  return dto;
}

function CreateContainerFlow({ onClose, memory, initialImage }: Omit<CreateContainerDialogProps, 'isOpen'>) {
  const t = useT();
  const [step, setStep] = useState(0);
  // The furthest step reached; going back leaves the later ones clickable.
  const [reached, setReached] = useState(0);
  const openStep = (index: number) => {
    setStep(index);
    setReached((furthest) => Math.max(furthest, index));
  };
  // The name follows the image until the user types their own.
  const [nameEdited, setNameEdited] = useState(false);
  const { register, control, getValues, setValue, setError, clearErrors, setFocus, subscribe, formState: { errors } } = useForm<CreateContainerForm>({
    defaultValues: initialImage ? { ...emptyForm, image_tag: initialImage, name: nameFromImage(initialImage) } : emptyForm,
  });
  const values = useWatch({ control }) as CreateContainerForm;
  const mutation = useCreateContainer();

  // A field's message goes once the field changes; Continue checks again.
  useEffect(() => subscribe({
    formState: { values: true },
    callback: ({ name }) => {
      if (name) clearErrors(name as Path<CreateContainerForm>);
    },
  }), [subscribe, clearErrors]);

  const showIssues = (target: CreateStep) => {
    const issues = stepIssues(getValues(), target, t, memory);
    clearErrors();
    for (const issue of issues) setError(issue.field as Path<CreateContainerForm>, { message: issue.message });
    if (issues.length > 0) setFocus(issues[0].field as Path<CreateContainerForm>);
    return issues.length === 0;
  };

  const submit = () => {
    const current = createSteps[step];
    if (current !== 'review') {
      if (showIssues(current)) openStep(step + 1);
      return;
    }
    // Every earlier step was checked on its way here; a step that changed
    // since is opened again with its messages.
    const failing = createSteps.findIndex((target) => stepIssues(getValues(), target, t, memory).length > 0);
    if (failing >= 0) {
      setStep(failing);
      showIssues(createSteps[failing]);
      return;
    }
    const parsed = createContainerSchema(t).parse(getValues());
    mutation.mutate(toDTO(parsed), { onSuccess: onClose });
  };

  const stepLabels = createSteps.map((id) => t(`containers.new.step.${id}`));

  return (
    <StepDialog
      isOpen
      onClose={onClose}
      title={t('containers.new.title')}
      steps={stepLabels}
      current={step}
      reached={reached}
      onStep={(index) => {
        clearErrors();
        setStep(index);
      }}
      onSubmit={submit}
      finishLabel={t('common.create')}
      isSubmitting={mutation.isPending}
    >
      {createSteps[step] === 'image' && (
        <ImageStep
          values={values}
          errors={{ image: errors.image_tag?.message, name: errors.name?.message }}
          onImage={(image) => {
            setValue('image_tag', image, { shouldDirty: true });
            clearErrors('image_tag');
            if (!nameEdited) {
              setValue('name', nameFromImage(image));
              clearErrors('name');
            }
          }}
          nameField={register('name', { onChange: (event) => setNameEdited(event.target.value !== '') })}
          imageRef={register('image_tag').ref}
        />
      )}
      {createSteps[step] === 'publishing' && (
        <PublishingStep
          values={values}
          errors={{ prefix: errors.domain_prefix?.message, port: errors.internal_port?.message }}
          onPublish={(publish) => {
            setValue('publish', publish);
            clearErrors(['domain_prefix', 'internal_port']);
            if (publish && !getValues('domain_prefix')) setValue('domain_prefix', subdomainFrom(getValues('name')));
          }}
          prefixField={register('domain_prefix')}
          portField={register('internal_port')}
        />
      )}
      {createSteps[step] === 'settings' && (
        <SettingsStep values={values} errors={errors} memory={memory} control={control} register={register} />
      )}
      {createSteps[step] === 'review' && <ReviewStep values={values} />}
    </StepDialog>
  );
}

type Registered = ReturnType<ReturnType<typeof useForm<CreateContainerForm>>['register']>;

function ImageStep({ values, errors, onImage, nameField, imageRef }: {
  values: CreateContainerForm;
  errors: { image?: string; name?: string };
  onImage: (image: string) => void;
  nameField: Registered;
  imageRef: Registered['ref'];
}) {
  const t = useT();
  const { data } = useImages('own', 1, 100);
  const ownImages = (data?.items ?? []).filter((image) => isImageAvailable(image.status)).map((image) => image.tag);
  return (
    <div className="grid gap-5">
      <Field label={t('containers.image')} htmlFor="create-image" hint={t('containers.new.imageHint')} error={errors.image}>
        <ImageField id="create-image" ref={imageRef} value={values.image_tag} onChange={onImage} ownImages={ownImages} />
      </Field>
      <Field label={t('containers.new.name')} htmlFor="create-name" hint={t('containers.new.nameHint')} error={errors.name}>
        <Input id="create-name" placeholder="my-app" autoComplete="off" spellCheck={false} error={!!errors.name} {...nameField} />
      </Field>
    </div>
  );
}

function PublishingStep({ values, errors, onPublish, prefixField, portField }: {
  values: CreateContainerForm;
  errors: { prefix?: string; port?: string };
  onPublish: (publish: boolean) => void;
  prefixField: Registered;
  portField: Registered;
}) {
  const t = useT();
  const preview = addressPreview(values, BASE_DOMAIN);
  return (
    <div className="grid gap-5">
      <ChoiceCards
        label={t('containers.new.step.publishing')}
        value={values.publish ? 'public' : 'private'}
        onChange={(choice) => onPublish(choice === 'public')}
        items={[
          { id: 'private', icon: Lock, title: t('containers.new.private'), description: t('containers.new.privateDescription') },
          { id: 'public', icon: Globe, title: t('containers.new.public'), description: t('containers.new.publicDescription') },
        ]}
      />
      {values.publish && (
        <>
          <div className="grid items-start gap-4 sm:grid-cols-[1fr_8rem]">
            <Field label={t('containers.new.subdomain')} htmlFor="create-subdomain" error={errors.prefix}>
              <Input id="create-subdomain" autoComplete="off" spellCheck={false} suffix={`.${BASE_DOMAIN}`} error={!!errors.prefix} {...prefixField} />
            </Field>
            <Field label={t('containers.new.port')} htmlFor="create-port" error={errors.port}>
              <Input id="create-port" inputMode="numeric" autoComplete="off" error={!!errors.port} {...portField} />
            </Field>
          </div>
          <p className="text-xs leading-snug text-label-2">{t('containers.new.portHint')}</p>
          {preview && (
            <p role="status" className="flex items-start gap-2 rounded-xl bg-fill px-3.5 py-2.5 leading-snug">
              <Globe aria-hidden className="mt-0.5 h-4 w-4 shrink-0 text-label-2" />
              <span className="min-w-0 break-words">
                {preview.port === undefined
                  ? t('containers.new.addressNoPort', { host: preview.host, name: preview.name })
                  : t('containers.new.address', { host: preview.host, port: preview.port, name: preview.name })}
              </span>
            </p>
          )}
        </>
      )}
    </div>
  );
}

function Section({ title, optional, children }: { title: string; optional?: boolean; children: ReactNode }) {
  const t = useT();
  return (
    <section className="grid gap-3">
      <h3 className="font-semibold">
        {title}
        {optional && <span className="ml-2 font-normal text-label-3">{t('common.optional')}</span>}
      </h3>
      {children}
    </section>
  );
}

function SettingsStep({ values, errors, memory, control, register }: {
  values: CreateContainerForm;
  errors: ReturnType<typeof useForm<CreateContainerForm>>['formState']['errors'];
  memory?: MemoryQuota;
  control: ReturnType<typeof useForm<CreateContainerForm>>['control'];
  register: ReturnType<typeof useForm<CreateContainerForm>>['register'];
}) {
  const t = useT();
  const { data } = useVolumes('own', 1, 100);
  const volumes = (data?.items ?? []).filter((volume) => isVolumeMountable(volume.status));
  const { fields: mounts, append, remove } = useFieldArray({ control, name: 'volume_mounts' });
  return (
    <div className="grid gap-7">
      <Section title={t('containers.new.resources')}>
        <div className="grid items-start gap-4 sm:grid-cols-2">
          <Field label={t('containers.memoryReservation')} htmlFor="create-memory" error={errors.memory_reservation_mb?.message}>
            <Input id="create-memory" inputMode="numeric" placeholder={t('containers.reservationDefault')} autoComplete="off" error={!!errors.memory_reservation_mb} {...register('memory_reservation_mb')} />
          </Field>
          <Field label={t('containers.cpuReservation')} htmlFor="create-cpu" error={errors.cpu_reservation_cores?.message}>
            <Input id="create-cpu" inputMode="decimal" placeholder={t('containers.reservationDefault')} autoComplete="off" error={!!errors.cpu_reservation_cores} {...register('cpu_reservation_cores')} />
          </Field>
        </div>
        {memory && <MemoryQuotaBar memory={memory} memoryMB={values.memory_reservation_mb} fieldError={!!errors.memory_reservation_mb} />}
        <p className="text-xs leading-snug text-label-2">{t('containers.new.reservationHint')}</p>
      </Section>

      <Section title={t('containers.envVars')} optional>
        <KeyValueFieldArray
          control={control}
          register={register}
          name="env_vars"
          keyPlaceholder={t('containers.envKeyPlaceholder')}
          valuePlaceholder={t('containers.envValuePlaceholder')}
          addLabel={t('containers.addEnv')}
          errors={errors.env_vars?.map?.((row) => row?.key?.message)}
        />
      </Section>

      <Section title={t('containers.volumes')} optional>
        {mounts.map((mount, index) => {
          const rowErrors = errors.volume_mounts?.[index];
          return (
            <div key={mount.id} className="grid grid-cols-[1fr_auto] items-start gap-2 sm:grid-cols-[1fr_1fr_auto_auto]">
              <Field error={rowErrors?.volume_id?.message} className="col-span-2 sm:col-span-1">
                <Select aria-label={t('containers.new.volume')} error={!!rowErrors?.volume_id} {...register(`volume_mounts.${index}.volume_id`)}>
                  <option value="">{t('containers.selectVolume')}</option>
                  {volumes.map((volume) => <option key={volume.id} value={volume.id}>{volume.name}</option>)}
                </Select>
              </Field>
              <Field error={rowErrors?.mount_path?.message}>
                <Input aria-label={t('containers.new.mountPath')} placeholder="/app/data" className="font-mono" autoComplete="off" spellCheck={false} error={!!rowErrors?.mount_path} {...register(`volume_mounts.${index}.mount_path`)} />
              </Field>
              <label className="inline-flex h-10 items-center gap-2 whitespace-nowrap text-label-2">
                <input type="checkbox" className="accent-accent-fill" {...register(`volume_mounts.${index}.is_readonly`)} />
                {t('containers.new.readOnly')}
              </label>
              <RemoveRowButton label={t('containers.new.removeMount')} onClick={() => remove(index)} />
            </div>
          );
        })}
        <AddRowButton label={t('containers.mountVolume')} onClick={() => append({ volume_id: '', mount_path: '', is_readonly: false })} />
      </Section>
    </div>
  );
}

// MemoryQuotaBar shows what the user's containers hold, what this one adds
// and what stays free; the added part turns red when it would not fit, with
// a line saying so until Continue puts the message under the field.
function MemoryQuotaBar({ memory, memoryMB, fieldError }: { memory: MemoryQuota; memoryMB: CreateContainerForm['memory_reservation_mb']; fieldError: boolean }) {
  const t = useT();
  const fit = memoryQuotaFit(memory, memoryMB);
  const amount = (bytes: number) => (bytes > 0 ? formatBytes(bytes, 1) : '0 MB');
  const parts = [{ value: fit.heldBytes, label: t('containers.new.quotaHeld', { amount: amount(fit.heldBytes) }), className: 'bg-label-3' }];
  if (fit.addBytes !== undefined) {
    parts.push({
      value: fit.addBytes,
      label: t('containers.new.quotaAdded', { amount: amount(fit.addBytes) }),
      className: fit.fits ? 'bg-accent-fill' : 'bg-red',
    });
  }
  return (
    <div className="grid gap-1.5">
      <div className="flex items-baseline justify-between gap-3 text-xs">
        <span className="font-medium text-label-2">{t('containers.new.memoryQuota')}</span>
        <span className="tabular-nums text-label-3">{amount(memory.quotaBytes)}</span>
      </div>
      <Meter parts={parts} total={memory.quotaBytes} freeLabel={t('containers.new.quotaFree', { amount: amount(fit.freeBytes) })} />
      {!fit.fits && !fieldError && fit.addBytes !== undefined && (
        <p className="text-xs leading-snug text-red-text">{t('containers.new.quotaOver', { amount: amount(fit.addBytes), free: amount(fit.availableBytes) })}</p>
      )}
      {fit.addBytes === undefined && <p className="text-xs leading-snug text-label-3">{t('containers.new.quotaDefault')}</p>}
    </div>
  );
}

function ReviewStep({ values }: { values: CreateContainerForm }) {
  const t = useT();
  const { data } = useVolumes('own', 1, 100);
  const volumeName = (id: string) => data?.items.find((volume) => volume.id === id)?.name ?? id;
  const preview = addressPreview(values, BASE_DOMAIN);
  const memoryMB = typedFigure(values.memory_reservation_mb);
  const cores = typedFigure(values.cpu_reservation_cores);
  const env = (values.env_vars ?? []).filter((row) => row.key);
  const mounts = values.volume_mounts ?? [];
  const none = <span className="text-label-3">{t('containers.new.none')}</span>;
  const facts: [string, ReactNode][] = [
    [t('containers.image'), <span className="break-all font-mono text-xs">{values.image_tag}</span>],
    [t('containers.new.name'), values.name],
    [t('containers.column.address'), preview
      ? <span>{preview.host} <span className="text-label-2">{t('containers.addressPort', { port: preview.port ?? '…' })}</span></span>
      : <span className="text-label-3">{t('containers.new.notPublished')}</span>],
    [t('containers.reserved'), [
      memoryMB === undefined ? t('containers.new.defaultMemory') : t('containers.new.reservedMemory', { memory: formatBytes(memoryMB * MiB, 1) }),
      cores === undefined ? t('containers.new.defaultCpu') : t('containers.new.reservedCpu', { cpu: formatCores(Math.round(cores * 1000)) }),
    ].join(' · ')],
    [t('containers.envVars'), env.length ? <span className="break-all font-mono text-xs">{env.map((row) => row.key).join(', ')}</span> : none],
    [t('containers.volumes'), mounts.length ? (
      <ul className="grid gap-0.5">
        {mounts.map((mount, index) => (
          <li key={index} className="break-all">
            {volumeName(mount.volume_id)} <span className="text-label-2">→</span> <span className="font-mono text-xs">{mount.mount_path}</span>
            {mount.is_readonly && <span className="text-label-2"> · {t('containers.new.readOnly')}</span>}
          </li>
        ))}
      </ul>
    ) : none],
  ];
  return (
    <dl className="grid gap-4">
      {facts.map(([label, value]) => (
        <div key={label} className="grid gap-0.5">
          <dt className="text-xs font-medium text-label-3">{label}</dt>
          <dd className="min-w-0">{value}</dd>
        </div>
      ))}
    </dl>
  );
}
