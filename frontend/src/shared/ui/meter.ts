// meterParts lays parts of one whole along a bar: each part's share in
// percent, in order, cut where the whole ends, and the share left free.
export function meterParts<P extends { value: number }>(parts: readonly P[], total: number) {
  let left = total > 0 ? 100 : 0;
  const laid = parts.map((part) => {
    const share = total > 0 ? (Math.max(part.value, 0) / total) * 100 : 0;
    const percent = Math.min(share, left);
    left -= percent;
    return { ...part, percent };
  });
  return { parts: laid, freePercent: left };
}
