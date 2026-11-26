// imageNameParts splits an image name after each slash, the slash kept on
// the part before it, so a long registry path can wrap at its segments and
// never inside one. Rendering puts a break opportunity between the parts.
export function imageNameParts(image: string): string[] {
  if (!image) return [];
  return image.split('/').map((part, index, parts) => (index < parts.length - 1 ? `${part}/` : part));
}
