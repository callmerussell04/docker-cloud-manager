import { Fragment, type ReactNode } from 'react';
import { dateLocale, useLocale, useT } from '@/shared/i18n';
import { ageOf } from '@/shared/lib/age';
import { BASE_DOMAIN } from '@/shared/lib/config';
import { useNowSeconds } from '@/shared/lib/hooks';
import { imageNameParts } from '@/shared/lib/imageName';
import { formatBytes } from '@/shared/lib/utils';
import { CopyButton } from '@/shared/ui/CopyButton';
import { formatCores, type AdminContainerData, type ContainerData } from '../model';
import { useAutoStopLabel } from './useAutoStopLabel';

interface ContainerFactsProps {
  container: ContainerData | AdminContainerData;
  // projectName names the container's Compose project, when it is known.
  projectName?: string;
}

// ContainerFacts stacks a container's details, label above value, so long
// values such as a registry image wrap whole instead of being cut off: the
// image (breaking only after a slash, with a copy button), its address and
// port, its reservation, its project, its auto-stop and when it was created.
export function ContainerFacts({ container, projectName }: ContainerFactsProps) {
  const t = useT();
  const locale = useLocale();
  const now = useNowSeconds(60_000);
  const autoStop = useAutoStopLabel(container.status, container.ttl_deadline);
  const address = container.domain_prefix ? `${container.domain_prefix}.${BASE_DOMAIN}` : '';
  const age = ageOf(container.created_at, now);
  const owner = 'owner_id' in container ? container.owner_username || container.owner_id : undefined;

  const facts: [string, ReactNode][] = [
    [
      t('containers.image'),
      <span className="flex items-start gap-1.5">
        {/* Each segment keeps together; a line may break only after a slash. */}
        <span className="min-w-0 flex-1 font-mono text-[12px]" title={container.image_tag}>
          {imageNameParts(container.image_tag).map((part, index) => (
            <Fragment key={index}>
              {index > 0 && <wbr />}
              <span className="whitespace-nowrap">{part}</span>
            </Fragment>
          ))}
        </span>
        <CopyButton value={container.image_tag} />
      </span>,
    ],
    [
      t('containers.column.address'),
      address ? (
        <>
          <a href={`http://${address}`} target="_blank" rel="noreferrer" title={address} className="block truncate font-medium text-accent hover:underline">
            {address}
          </a>
          <div className="text-label-2">{t('containers.addressPort', { port: container.internal_port })}</div>
        </>
      ) : (
        <span className="text-label-3">{t('common.notRouted')}</span>
      ),
    ],
    [
      t('containers.reserved'),
      t('containers.reservationValue', { memory: formatBytes(container.memory_reservation_bytes, 1), cpu: formatCores(container.cpu_reservation_millicores) }),
    ],
    [
      t('containers.project'),
      container.project_id ? projectName ?? t('containers.composeTag') : <span className="text-label-3">{t('containers.standalone')}</span>,
    ],
    ...(autoStop ? [[t('containers.autoStop'), autoStop] as [string, ReactNode]] : []),
    ...(owner ? [[t('admin.resources.owner'), owner] as [string, ReactNode]] : []),
    [
      t('containers.column.created'),
      t('containers.createdValue', {
        date: new Date(container.created_at * 1000).toLocaleString(dateLocale(locale), { dateStyle: 'medium', timeStyle: 'short' }),
        age: t(`common.age.${age.unit}`, { count: age.count }),
      }),
    ],
  ];

  return (
    <dl>
      {facts.map(([label, value]) => (
        <div key={label} className="grid gap-[3px] border-t border-separator px-0.5 py-2.5 first:border-t-0 first:pt-0">
          <dt className="text-[12px] font-semibold text-label-2">{label}</dt>
          <dd className="min-w-0 leading-[1.45] [overflow-wrap:anywhere]">{value}</dd>
        </div>
      ))}
    </dl>
  );
}
