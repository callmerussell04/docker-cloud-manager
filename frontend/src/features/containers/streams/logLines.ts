// A line of a container's log as the console keeps it. The stream is asked
// for timestamps, so the console can show or hide them without reconnecting.
export interface LogLine {
  // id is the line's place in the stream, a stable key as old lines drop off.
  id: number;
  timestamp?: string;
  text: string;
}

// Docker puts an RFC 3339 timestamp and one space before each line.
const timestampPattern = /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})) ?/;

// splitLogTimestamp parts a streamed line into its leading timestamp, when it
// has one, and its text.
export function splitLogTimestamp(raw: string): Omit<LogLine, 'id'> {
  const match = timestampPattern.exec(raw);
  if (!match) return { timestamp: undefined, text: raw };
  return { timestamp: match[1], text: raw.slice(match[0].length) };
}

// filterLogLines keeps the lines whose text holds the filter, ignoring case;
// a blank filter keeps them all.
export function filterLogLines(lines: readonly LogLine[], filter: string): readonly LogLine[] {
  const query = filter.trim().toLowerCase();
  if (!query) return lines;
  return lines.filter((line) => line.text.toLowerCase().includes(query));
}

// How far from the bottom, in pixels, still counts as reading the newest line.
const latestSlack = 24;

// isAtLatest reports whether a scrolled log shows its newest line, so it
// keeps following new lines; scrolled further up, the reader is on older ones.
export function isAtLatest({ scrollHeight, scrollTop, clientHeight }: { scrollHeight: number; scrollTop: number; clientHeight: number }) {
  return scrollHeight - scrollTop - clientHeight <= latestSlack;
}
