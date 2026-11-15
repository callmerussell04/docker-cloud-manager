import { z } from 'zod';

import { sections, type ListItemKind, type SettingDef } from './catalogue';
import type { SettingName, SystemConfig } from './model';

/**
 * Why a value is refused. The page turns a code into a sentence with the
 * setting's range, in the unit the setting is shown in.
 */
export type SettingErrorCode =
  | 'required' | 'integer' | 'range' | 'size' | 'listRequired' | 'listItem' | 'listUnique' | 'boostFloor';

const code = (message: SettingErrorCode) => ({ message });

// The format Core accepts for Docker sizes.
const dockerSize = /^[1-9][0-9]*[kKmMgG]$/;

// The same checks Core makes on each entry of a list. A pattern is checked
// with the browser's regular expressions, which accept a little more than
// Core's; Core has the last word.
const listItemRules: Record<ListItemKind, { valid: (item: string) => boolean; key: (item: string) => string }> = {
  prefix: {
    valid: (item) => /^[a-z0-9]([a-z0-9-]{0,28}[a-z0-9])?$/.test(item),
    key: (item) => item.toLowerCase(),
  },
  pattern: {
    valid: (item) => {
      try {
        new RegExp(`^(?:${item})$`);
        return true;
      } catch {
        return false;
      }
    },
    key: (item) => item,
  },
  host: {
    // No scheme, port, path or spaces, and no dot at either end.
    valid: (item) => !/[\s:/\\\0]/.test(item) && !item.startsWith('.') && !item.endsWith('.'),
    key: (item) => item.toLowerCase(),
  },
  command: {
    valid: (item) => !/[\0\r\n]/.test(item),
    key: (item) => item,
  },
};

function listSchema(setting: SettingDef) {
  const rules = setting.item ? listItemRules[setting.item] : undefined;
  const list = z.array(z.string().min(1, code('listItem')));
  return (setting.required ? list.min(1, code('listRequired')) : list).superRefine((items, context) => {
    if (!rules) return;
    if (!items.every(rules.valid)) {
      context.addIssue({ code: 'custom', ...code('listItem') });
    } else if (new Set(items.map(rules.key)).size !== items.length) {
      context.addIssue({ code: 'custom', ...code('listUnique') });
    }
  });
}

function numberSchema(setting: SettingDef) {
  let schema = z.number(code('required'));
  if (setting.kind !== 'decimal') schema = schema.int(code('integer'));
  if (setting.min !== undefined) {
    schema = setting.aboveMin ? schema.gt(setting.min, code('range')) : schema.min(setting.min, code('range'));
  }
  if (setting.max !== undefined) schema = schema.max(setting.max, code('range'));
  return schema;
}

function settingSchema(setting: SettingDef) {
  switch (setting.kind) {
    case 'toggle':
      return z.boolean(code('required'));
    case 'size':
      return z.string(code('required')).regex(dockerSize, code('size'));
    case 'list':
      return listSchema(setting);
    default:
      return numberSchema(setting);
  }
}

const shape = Object.fromEntries(
  sections.flatMap((section) => section.settings.map((setting) => [setting.name, settingSchema(setting)])),
) as unknown as Record<SettingName, z.ZodType>;

// The same rules Core applies to an update, so the page refuses what the
// server would. Each setting's own rule comes from the catalogue; the two
// rules below span settings.
export const systemConfigSchema = z.object(shape).superRefine((value, context) => {
  const settings = value as SystemConfig;
  if (settings.boost_min_free_memory_bytes < settings.memory_pressure_min_free_bytes) {
    context.addIssue({ code: 'custom', path: ['boost_min_free_memory_bytes'], ...code('boostFloor') });
  }
  if (settings.git_sources_enabled && settings.git_allowed_hosts.length === 0) {
    context.addIssue({ code: 'custom', path: ['git_allowed_hosts'], ...code('listRequired') });
  }
}) as unknown as z.ZodType<SystemConfig, SystemConfig>;
