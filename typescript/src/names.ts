/**
 * Step-name validation. Names must match /^[A-Za-z0-9][A-Za-z0-9._-]*$/.
 * Duplicate names within one task body raise MundaneDuplicateStepError;
 * see ../src/index.ts.
 */

const NAME_RE = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

export function validateName(name: string): void {
  if (typeof name !== "string" || !NAME_RE.test(name)) {
    throw new Error(`invalid step name ${JSON.stringify(name)}: must match ${NAME_RE.source}`);
  }
}
