import { describe, expect, it } from 'vitest';
import { imageNameParts } from './imageName';

describe('imageNameParts', () => {
  it('ends every part but the last with its slash, so a line breaks only after one', () => {
    expect(imageNameParts('registry.example.com:5000/team/web-shop:1.4.2')).toEqual([
      'registry.example.com:5000/',
      'team/',
      'web-shop:1.4.2',
    ]);
  });

  it('keeps a name without a slash whole', () => {
    expect(imageNameParts('nginx:1.27-alpine')).toEqual(['nginx:1.27-alpine']);
  });

  it('keeps a digest with its last segment', () => {
    expect(imageNameParts('ghcr.io/acme/api@sha256:abc')).toEqual(['ghcr.io/', 'acme/', 'api@sha256:abc']);
  });

  it('gives nothing for an empty name', () => {
    expect(imageNameParts('')).toEqual([]);
  });
});
