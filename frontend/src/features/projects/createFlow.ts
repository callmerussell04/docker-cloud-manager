import type { TFunction } from '@/shared/i18n';
import { deployProjectSchema, maxProjectNameLength, type DeployProjectSchemaForm } from './schemas';

// The deploy project dialog's steps, in order.
export const deploySteps = ['source', 'project', 'review'] as const;
export type DeployStep = (typeof deploySteps)[number];

// DeploySource is where the Compose project comes from: an uploaded archive
// or YAML file, or a public Git repository.
export type DeploySource = 'upload' | 'git';

export type DeployProjectForm = DeployProjectSchemaForm;

// DeployField is a place a step's message is shown: a form field, the file
// chooser, or the source choice itself.
export type DeployField = keyof DeployProjectForm | 'file';

export interface DeployStepIssue {
  field: DeployField;
  message: string;
}

export interface DeployContext {
  // file is the chosen upload, if any.
  file: { name: string } | null;
  // gitEnabled is false while the administrator has turned Git sources off.
  gitEnabled: boolean;
}

const archiveExtensions = ['.zip', '.tar', '.tar.gz', '.tgz'];
const composeExtensions = ['.yml', '.yaml'];

// isRawComposeFile reports whether an upload is a Compose YAML file rather
// than an archive; a YAML file is the Compose file itself, so it has no path.
export function isRawComposeFile(fileName: string) {
  const lower = fileName.toLowerCase();
  return composeExtensions.some((extension) => lower.endsWith(extension));
}

// isProjectSourceFile reports whether Core takes the file as a project
// source: an archive or a Compose YAML file, by its name.
export function isProjectSourceFile(fileName: string) {
  const lower = fileName.toLowerCase();
  return isRawComposeFile(lower) || archiveExtensions.some((extension) => lower.endsWith(extension));
}

// deployStepIssues is what keeps a step from moving on: on Source, a missing
// or unsupported file, a missing repository URL, or Git while it is turned
// off; on Project, the schema's message for the name.
export function deployStepIssues(values: DeployProjectForm, step: DeployStep, t: TFunction, context: DeployContext): DeployStepIssue[] {
  if (step === 'source') {
    if (values.source === 'git') {
      if (!context.gitEnabled) return [{ field: 'source', message: t('common.gitUnavailable') }];
      if (!values.repo_url.trim()) return [{ field: 'repo_url', message: t('validation.gitUrlRequired') }];
      return [];
    }
    if (!context.file) return [{ field: 'file', message: t('validation.fileRequired') }];
    if (!isProjectSourceFile(context.file.name)) return [{ field: 'file', message: t('validation.projectFileType') }];
    return [];
  }
  if (step === 'project') {
    const result = deployProjectSchema(t).safeParse(values);
    if (result.success) return [];
    return result.error.issues
      .filter((issue) => issue.path[0] === 'project_name')
      .slice(0, 1)
      .map((issue) => ({ field: 'project_name' as const, message: issue.message }));
  }
  return [];
}

const sourceExtensions = [...archiveExtensions, ...composeExtensions].sort((a, b) => b.length - a.length);

// projectNameFrom suggests a project name from its source: the file's name
// without its extension, or the repository's last path part without .git,
// lowercased and with every character Core refuses turned into a hyphen.
// It is empty when nothing usable is left.
export function projectNameFrom(input: { source: DeploySource; fileName?: string; repoUrl?: string }) {
  let base = '';
  if (input.source === 'upload' && input.fileName) {
    const lower = input.fileName.toLowerCase();
    const extension = sourceExtensions.find((entry) => lower.endsWith(entry));
    base = extension ? lower.slice(0, -extension.length) : lower;
  } else if (input.source === 'git' && input.repoUrl) {
    base = (input.repoUrl.trim().replace(/\/+$/, '').split('/').pop() ?? '').toLowerCase().replace(/\.git$/, '');
  }
  return base
    .replace(/[^a-z0-9_.-]+/g, '-')
    .replace(/^[^a-z0-9]+/, '')
    .replace(/-+$/, '')
    .slice(0, maxProjectNameLength);
}
