import { describe, expect, it } from 'vitest';
import { filterLogLines, isAtLatest, splitLogTimestamp, type LogLine } from './logLines';

describe('splitLogTimestamp', () => {
  it('takes the leading Docker timestamp off the line', () => {
    expect(splitLogTimestamp('2026-11-23T10:00:01.123456789Z GET /healthz 200')).toEqual({
      timestamp: '2026-11-23T10:00:01.123456789Z',
      text: 'GET /healthz 200',
    });
  });

  it('keeps a line without a timestamp as its text', () => {
    expect(splitLogTimestamp('listening on :3000')).toEqual({ timestamp: undefined, text: 'listening on :3000' });
  });

  it('keeps an empty line after its timestamp', () => {
    expect(splitLogTimestamp('2026-11-23T10:00:01Z ')).toEqual({ timestamp: '2026-11-23T10:00:01Z', text: '' });
  });
});

describe('filterLogLines', () => {
  const lines: LogLine[] = [
    { id: 1, timestamp: '2026-11-23T10:00:01Z', text: 'GET /api/cart 200' },
    { id: 2, timestamp: '2026-11-23T10:00:02Z', text: 'Payment provider TIMEOUT' },
    { id: 3, timestamp: undefined, text: 'GET /healthz 200' },
  ];

  it('keeps every line while the filter is empty or blank', () => {
    expect(filterLogLines(lines, '')).toEqual(lines);
    expect(filterLogLines(lines, '   ')).toEqual(lines);
  });

  it('matches the text regardless of case', () => {
    expect(filterLogLines(lines, 'timeout').map((line) => line.id)).toEqual([2]);
    expect(filterLogLines(lines, 'GET').map((line) => line.id)).toEqual([1, 3]);
  });

  it('does not match the timestamp', () => {
    expect(filterLogLines(lines, '2026')).toEqual([]);
  });
});

describe('isAtLatest', () => {
  it('counts a view scrolled to the bottom, or within a line of it, as at the latest line', () => {
    expect(isAtLatest({ scrollHeight: 1000, scrollTop: 600, clientHeight: 400 })).toBe(true);
    expect(isAtLatest({ scrollHeight: 1000, scrollTop: 580, clientHeight: 400 })).toBe(true);
  });

  it('counts a view scrolled further up as reading older lines', () => {
    expect(isAtLatest({ scrollHeight: 1000, scrollTop: 500, clientHeight: 400 })).toBe(false);
  });
});
