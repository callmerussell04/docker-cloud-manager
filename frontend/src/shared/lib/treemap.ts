// Rect is a block of a treemap, in the units of the rectangle it divides.
export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

// worst is the largest aspect ratio among a row's blocks when the row is laid
// along a side of the given length: the squarified treemap keeps it low.
function worst(row: readonly number[], side: number) {
  const sum = row.reduce((total, value) => total + value, 0);
  const largest = Math.max(...row);
  const smallest = Math.min(...row);
  return Math.max((side * side * largest) / (sum * sum), (sum * sum) / (side * side * smallest));
}

// treemap divides a width-by-height rectangle among sizes, each block's area
// in proportion to its size, keeping blocks close to square (the squarified
// treemap of Bruls, Huizing and van Wijk). Blocks come back in the order of
// the sizes and are placed in that order from the top left, so a caller that
// sorts the largest first gets the best shapes and its last block at the
// bottom right. A size of zero or less gets an empty rectangle.
export function treemap(sizes: readonly number[], width: number, height: number): Rect[] {
  const rects: Rect[] = sizes.map(() => ({ x: 0, y: 0, width: 0, height: 0 }));
  const shown = sizes.flatMap((size, index) => (size > 0 ? [index] : []));
  const total = shown.reduce((sum, index) => sum + sizes[index], 0);
  if (total <= 0 || width <= 0 || height <= 0) return rects;

  const scale = (width * height) / total;
  let free: Rect = { x: 0, y: 0, width, height };
  let row: number[] = [];

  // place lays the row along the free rectangle's shorter side and leaves the
  // rest of it free.
  const place = () => {
    const areas = row.map((index) => sizes[index] * scale);
    const sum = areas.reduce((a, b) => a + b, 0);
    const across = free.width >= free.height;
    const depth = sum / (across ? free.height : free.width);
    let offset = 0;
    row.forEach((index, at) => {
      const length = areas[at] / depth;
      rects[index] = across
        ? { x: free.x, y: free.y + offset, width: depth, height: length }
        : { x: free.x + offset, y: free.y, width: length, height: depth };
      offset += length;
    });
    free = across
      ? { x: free.x + depth, y: free.y, width: free.width - depth, height: free.height }
      : { x: free.x, y: free.y + depth, width: free.width, height: free.height - depth };
    row = [];
  };

  for (const index of shown) {
    const side = Math.min(free.width, free.height);
    const areas = row.map((at) => sizes[at] * scale);
    const next = sizes[index] * scale;
    if (row.length > 0 && worst([...areas, next], side) > worst(areas, side)) place();
    row.push(index);
  }
  place();
  return rects;
}
