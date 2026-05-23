/**
 * Custom error classes thrown by mundane.
 *
 * - MundaneLockedError: another live process holds the file lock.
 * - MundaneSerializationError: a step's return value doesn't survive a
 *   JSON round-trip (Date, undefined, BigInt, Map, Set, function, circular).
 * - MundaneSchemaError: file exists but meta.schema_version != "1".
 * - MundaneStepFailedError: a step body threw; wraps the underlying error.
 */

export class MundaneLockedError extends Error {
  readonly code = "EMUNDANELOCKED";
  constructor(message: string) {
    super(message);
    this.name = "MundaneLockedError";
  }
}

export class MundaneSerializationError extends Error {
  readonly code = "EMUNDANESERIALIZATION";
  readonly path?: string;
  constructor(message: string, path?: string) {
    super(message);
    this.name = "MundaneSerializationError";
    this.path = path;
  }
}

export class MundaneSchemaError extends Error {
  readonly code = "EMUNDANESCHEMA";
  constructor(message: string) {
    super(message);
    this.name = "MundaneSchemaError";
  }
}

export class MundaneStepFailedError extends Error {
  readonly code = "EMUNDANESTEPFAILED";
  readonly stepName: string;
  readonly original: unknown;
  constructor(stepName: string, original: unknown) {
    const msg =
      original instanceof Error
        ? `step ${JSON.stringify(stepName)} failed: ${original.message}`
        : `step ${JSON.stringify(stepName)} failed: ${String(original)}`;
    super(msg);
    this.name = "MundaneStepFailedError";
    this.stepName = stepName;
    this.original = original;
    if (original instanceof Error && original.stack) {
      this.stack = `${this.stack}\nCaused by: ${original.stack}`;
    }
  }
}
