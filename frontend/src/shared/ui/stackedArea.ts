// The geometry of a stacked area chart (StackedAreaChart), kept apart from
// its drawing so it can be tested.

export interface Band {
  // lower and upper are the band's bottom and top at each point.
  lower: number[];
  upper: number[];
}

// stackBands lays each series on top of the ones before it: the first band
// starts at zero, and each later one starts where the one below ends.
export function stackBands(series: readonly (readonly number[])[]): Band[] {
  const bands: Band[] = [];
  let below = (series[0] ?? []).map(() => 0);
  for (const values of series) {
    const upper = values.map((value, index) => (below[index] ?? 0) + value);
    bands.push({ lower: below, upper });
    below = upper;
  }
  return bands;
}

const niceSteps = [1, 2, 2.5, 5, 10];

// niceCeiling is the top of a chart's scale: the smallest of 1, 2, 2.5 or 5
// times a power of ten that is at least max, so the guides fall on round
// figures.
export function niceCeiling(max: number) {
  if (!(max > 0)) return 1;
  const power = 10 ** Math.floor(Math.log10(max));
  const step = niceSteps.find((candidate) => candidate * power >= max - 1e-9) ?? 10;
  return step * power;
}

// ScaleKind is what a chart's figures are: bytes, rounded in their own
// binary unit (KB, MB, GB as formatBytes writes them), or plain figures.
export type ScaleKind = 'bytes' | 'plain';

// niceScale is the top of a chart's scale for its largest figure: a round
// number of the unit the figures are read in, so the guides fall on figures
// such as 5 GB or 2.5 GB rather than on round numbers of bytes.
export function niceScale(max: number, kind: ScaleKind) {
  const unit = unitOf(max, kind);
  const top = niceCeiling(max / unit) * unit;
  // Just under a unit (1010 MB) rounds past it; read it in the next unit.
  return kind === 'bytes' && top > unit * 1024 ? niceCeiling(max / (unit * 1024)) * unit * 1024 : top;
}

// unitOf is the unit a figure is read in: for bytes the binary unit
// formatBytes writes it in (1024 to the power of its KB, MB, GB step).
function unitOf(value: number, kind: ScaleKind) {
  if (kind === 'plain' || !(value > 0)) return 1;
  return 1024 ** Math.max(0, Math.floor(Math.log(value) / Math.log(1024)));
}

// guideShares are where a chart's guides fall, as shares of its scale's
// top, so each falls on a round figure of the unit: quarters on a top of 1
// or 2 times a power of ten, fifths on 2.5 or 5; a short chart draws every
// other one.
export function guideShares(ceiling: number, kind: ScaleKind, tall: boolean) {
  const figure = ceiling / unitOf(ceiling, kind);
  const mantissa = figure / 10 ** Math.floor(Math.log10(figure));
  const quarters = Math.abs(mantissa - 1) < 1e-9 || Math.abs(mantissa - 2) < 1e-9;
  if (quarters) return tall ? [0, 0.25, 0.5, 0.75, 1] : [0, 0.5, 1];
  return tall ? [0, 0.2, 0.4, 0.6, 0.8, 1] : [0, 0.4, 0.8];
}

interface Cluster {
  first: number;
  // originals are the wanted positions of the cluster's labels.
  originals: number[];
  start: number;
}

function centred(originals: readonly number[], gap: number) {
  const mean = originals.reduce((sum, value) => sum + value, 0) / originals.length;
  return mean - ((originals.length - 1) * gap) / 2;
}

// spreadLabels moves labels wanted at the given positions (in order, top to
// bottom) so that neighbours are at least gap apart and all lie between min
// and max. Labels that crowd each other spread evenly around where they
// wanted to be; the rest stay put.
export function spreadLabels(positions: readonly number[], gap: number, min: number, max: number): number[] {
  const clusters: Cluster[] = [];
  positions.forEach((position, index) => {
    let cluster: Cluster = { first: index, originals: [position], start: position };
    for (;;) {
      const previous = clusters[clusters.length - 1];
      if (!previous || previous.start + (previous.originals.length - 1) * gap + gap <= cluster.start) break;
      clusters.pop();
      const originals = [...previous.originals, ...cluster.originals];
      cluster = { first: previous.first, originals, start: centred(originals, gap) };
    }
    clusters.push(cluster);
  });
  const laid = clusters.flatMap((cluster) => cluster.originals.map((_, index) => cluster.start + index * gap));
  // Pull the labels inside the plot, the bottom first, then the top.
  for (let index = laid.length - 1; index >= 0; index--) {
    const limit = index === laid.length - 1 ? max : laid[index + 1] - gap;
    laid[index] = Math.min(laid[index], limit);
  }
  for (let index = 0; index < laid.length; index++) {
    const limit = index === 0 ? min : laid[index - 1] + gap;
    laid[index] = Math.max(laid[index], limit);
  }
  return laid;
}

// nearestPoint is the index of the time nearest the one under the pointer,
// or -1 when there are no points.
export function nearestPoint(times: readonly number[], time: number) {
  let best = -1;
  times.forEach((candidate, index) => {
    if (best < 0 || Math.abs(candidate - time) < Math.abs(times[best] - time)) best = index;
  });
  return best;
}
