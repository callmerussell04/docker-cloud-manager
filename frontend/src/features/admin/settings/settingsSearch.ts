import { sectionOf, sections, type SectionId, type SettingDef } from './catalogue';
import type { SettingName } from './model';

export interface FoundSection {
  id: SectionId;
  settings: SettingDef[];
}

/**
 * The runtime settings a search finds, by section in page order, each section
 * with only its matching settings; null while the query is blank, which is no
 * search at all. Every word of the query must appear, in any order and without
 * case, in the setting's name (underscores read as spaces) or in one of the
 * texts `textsOf` gives for it: its label and description in the reader's
 * language.
 */
export function searchSettings(query: string, textsOf: (setting: SettingDef) => string[]): FoundSection[] | null {
  const words = query.toLowerCase().replaceAll('_', ' ').split(/\s+/).filter(Boolean);
  if (words.length === 0) return null;
  const matches = (setting: SettingDef) => {
    const haystack = [setting.name.replaceAll('_', ' '), ...textsOf(setting)].join('\n').toLowerCase();
    return words.every((word) => haystack.includes(word));
  };
  return sections
    .map((section) => ({ id: section.id, settings: section.settings.filter(matches) }))
    .filter((section) => section.settings.length > 0);
}

/**
 * How many of the named settings each section holds, leaving out sections
 * with none and names that are not runtime settings: the changed, refused or
 * found settings a section list shows beside each section.
 */
export function countBySection(names: Iterable<string>): Partial<Record<SectionId, number>> {
  const counts: Partial<Record<SectionId, number>> = {};
  for (const name of names) {
    const section = Object.hasOwn(sectionOf, name) ? sectionOf[name as SettingName] : undefined;
    if (section) counts[section] = (counts[section] ?? 0) + 1;
  }
  return counts;
}
