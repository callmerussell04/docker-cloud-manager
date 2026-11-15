import type { validation as ru } from '../ru/validation';

export const validation: Record<keyof typeof ru, string> = {
  'validation.usernameRequired': 'Username is required',
  'validation.usernameMin3': 'Username must be at least 3 characters',
  'validation.emailInvalid': 'Enter a valid email address',
  'validation.passwordMin6': 'Password must be at least 6 characters',
  'validation.passwordRequired': 'Password is required for a new user',
  'validation.quotaPositive': 'Enter a positive number (CPU from 0.001 to 1,000,000 cores)',
  'validation.nameRequired': 'Name is required',
  'validation.projectNameRequired': 'Project name is required',
  'validation.imageRequired': 'Image is required',
  'validation.keyRequired': 'Key is required',
  'validation.volumeRequired': 'Select a volume',
  'validation.pathRequired': 'Path is required',
  'validation.prefixRequired': 'Prefix is required',
  'validation.max30': 'Maximum 30 characters',
  'validation.fileRequired': 'Please choose a file',
  'validation.archiveRequired': 'Please choose a source archive',
  'validation.gitUrlRequired': 'Enter a Git repository URL',
  'validation.projectFileType': 'Only archives (.zip, .tar.gz) or .yml files are allowed',
  'validation.buildFileType': 'Only .zip, .tar, .tar.gz archives are allowed',
  'validation.portRange': 'Enter a port from 1 to 65535',
};
