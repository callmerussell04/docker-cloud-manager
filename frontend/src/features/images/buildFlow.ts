import type { TFunction } from '@/shared/i18n';
import { createBuildSchema, type CreateBuildForm } from './schemas';

// The build image dialog's steps, in order.
export const buildSteps = ['source', 'image', 'review'] as const;
export type BuildStep = (typeof buildSteps)[number];

// BuildSource is where the build's context comes from: an uploaded archive
// or a public Git repository.
export type BuildSource = 'archive' | 'git';

export type BuildImageForm = CreateBuildForm;

// BuildField is a place a step's message is shown: a form field (a build
// argument's name by its path), the file chooser, or the source choice.
export type BuildField = 'source' | 'file' | 'repo_url' | 'tag' | `build_args.${number}.key`;

export interface BuildStepIssue {
  field: BuildField;
  message: string;
}

export interface BuildContext {
  // file is the chosen archive, if any.
  file: { name: string } | null;
  // gitEnabled is false while the administrator has turned Git sources off.
  gitEnabled: boolean;
}

const archiveExtensions = ['.zip', '.tar', '.tar.gz', '.tgz'];

// isBuildArchive reports whether Core takes the file as a build archive, by
// its name.
export function isBuildArchive(fileName: string) {
  const lower = fileName.toLowerCase();
  return archiveExtensions.some((extension) => lower.endsWith(extension));
}

// buildStepIssues is what keeps a step from moving on: on Source, a missing
// or unsupported archive, a missing repository URL, or Git while it is
// turned off; on Image, the schema's messages for the tag and each build
// argument's name.
export function buildStepIssues(values: BuildImageForm, step: BuildStep, t: TFunction, context: BuildContext): BuildStepIssue[] {
  if (step === 'source') {
    if (values.source === 'git') {
      if (!context.gitEnabled) return [{ field: 'source', message: t('common.gitUnavailable') }];
      if (!values.repo_url.trim()) return [{ field: 'repo_url', message: t('validation.gitUrlRequired') }];
      return [];
    }
    if (!context.file) return [{ field: 'file', message: t('validation.fileRequired') }];
    if (!isBuildArchive(context.file.name)) return [{ field: 'file', message: t('validation.buildFileType') }];
    return [];
  }
  if (step === 'image') {
    const result = createBuildSchema(t).safeParse(values);
    if (result.success) return [];
    return result.error.issues
      .filter((issue) => issue.path[0] === 'tag' || (issue.path[0] === 'build_args' && issue.path[2] === 'key'))
      .map((issue) => ({ field: issue.path.join('.') as BuildField, message: issue.message }));
  }
  return [];
}
