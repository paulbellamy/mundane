/**
 * Parse duration strings like "30s", "5m", "2h", "1d", "500ms".
 *
 * Returns the number of milliseconds. Throws on unparseable input.
 */

const UNIT_MS: Record<string, number> = {
  ms: 1,
  s: 1_000,
  m: 60_000,
  h: 3_600_000,
  d: 86_400_000,
};

const RE = /^\s*(\d+(?:\.\d+)?)(ms|s|m|h|d)\s*$/i;

export function parseDurationMs(input: string | number): number {
  if (typeof input === "number") {
    if (!Number.isFinite(input)) throw new TypeError("duration: non-finite number");
    return Math.trunc(input);
  }
  const m = RE.exec(input);
  if (!m) {
    throw new Error(
      `invalid duration ${JSON.stringify(input)}: expected e.g. "500ms", "30s", "5m", "2h", "1d"`,
    );
  }
  const magnitude = Number(m[1]);
  const unit = m[2].toLowerCase();
  return Math.round(magnitude * UNIT_MS[unit]);
}
