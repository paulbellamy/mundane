/**
 * Step-name validation and disambiguation.
 *
 * Names must match /^[A-Za-z0-9][A-Za-z0-9._-]*$/. Duplicates within the
 * same task body get "#2", "#3", ... appended in caller order.
 */

const NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._\-]*$/;

export function validateName(name: string): void {
  if (typeof name !== "string" || !NAME_RE.test(name)) {
    throw new Error(
      `invalid step name ${JSON.stringify(name)}: must match ${NAME_RE.source}`,
    );
  }
}

export class Disambiguator {
  private counts = new Map<string, number>();
  next(base: string): string {
    const n = (this.counts.get(base) ?? 0) + 1;
    this.counts.set(base, n);
    return n === 1 ? base : `${base}#${n}`;
  }
}
