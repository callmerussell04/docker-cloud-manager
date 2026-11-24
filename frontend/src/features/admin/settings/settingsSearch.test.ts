import { describe, expect, it } from 'vitest';
import type { SettingDef } from './catalogue';
import { countBySection, searchSettings } from './settingsSearch';

// The texts a person reads for a setting: its label and description. These
// stand in for the dictionaries, so the tests do not follow their wording.
const texts: Partial<Record<string, [string, string]>> = {
  default_user_quota_ram_mb: ['Default memory quota', 'Memory a user may reserve.'],
  max_burst_multiplier: ['Limit multiplier', 'Standard memory limit of a container.'],
  max_cpu_burst_multiplier: ['Limit multiplier', 'CPU ceiling of a container.'],
  git_allowed_hosts: ['Allowed hosts', 'Hosts users may clone from.'],
};
const textsOf = (setting: SettingDef) => texts[setting.name] ?? ['', ''];

const found = (query: string) =>
  searchSettings(query, textsOf)?.map((section) => [section.id, section.settings.map((setting) => setting.name)]);

describe('searchSettings', () => {
  it('finds a setting by its label, ignoring case', () => {
    expect(found('allowed HOSTS')).toEqual([['git', ['git_allowed_hosts']]]);
  });

  it('finds a setting by its description', () => {
    expect(found('clone from')).toEqual([['git', ['git_allowed_hosts']]]);
  });

  it('finds a setting by its name, underscores read as spaces', () => {
    expect(found('pids')).toEqual([['containers', ['container_pids_limit']]]);
    expect(found('pids_limit')).toEqual([['containers', ['container_pids_limit']]]);
    expect(found('pids limit')).toEqual([['containers', ['container_pids_limit']]]);
  });

  it('keeps every section with a match, in page order, each with only its matches', () => {
    expect(found('limit multiplier')).toEqual([
      ['memory', ['max_burst_multiplier']],
      ['cpu', ['max_cpu_burst_multiplier']],
    ]);
  });

  it('needs every word, in any order and across label and description', () => {
    expect(found('container ceiling')).toEqual([['cpu', ['max_cpu_burst_multiplier']]]);
    expect(found('multiplier hosts')).toEqual([]);
  });

  it('is not a search while the query is blank', () => {
    expect(searchSettings('', textsOf)).toBeNull();
    expect(searchSettings('   ', textsOf)).toBeNull();
  });
});

describe('countBySection', () => {
  it('counts settings per section, leaving out sections with none', () => {
    expect(countBySection(['max_burst_multiplier', 'overcommit_factor', 'git_allowed_hosts'])).toEqual({
      memory: 2,
      git: 1,
    });
  });

  it('ignores names that are not runtime settings', () => {
    expect(countBySection(['local_login_enabled', 'container_ttl_hours'])).toEqual({ containers: 1 });
  });

  it('is empty for no settings', () => {
    expect(countBySection([])).toEqual({});
  });
});
