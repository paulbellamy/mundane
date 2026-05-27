/**
 * Custom error classes thrown by mundane.
 *
 * - LockedError: another live process holds the file lock.
 * - SerializationError: a step's return value doesn't survive a
 *   JSON round-trip (Date, undefined, BigInt, Map, Set, function, circular).
 * - SchemaError: file exists but meta.schema_version != "1".
 * - StepFailedError: a step body threw; wraps the underlying error.
 */

export class LockedError extends Error {
  readonly code = "EMUNDANELOCKED";
  constructor(message: string) {
    super(message);
    this.name = "LockedError";
  }
}

export class SerializationError extends Error {
  readonly code = "EMUNDANESERIALIZATION";
  readonly path?: string;
  constructor(message: string, path?: string) {
    super(message);
    this.name = "SerializationError";
    this.path = path;
  }
}

export class SchemaError extends Error {
  readonly code = "EMUNDANESCHEMA";
  constructor(message: string) {
    super(message);
    this.name = "SchemaError";
  }
}

export class DuplicateStepError extends Error {
  readonly code = "EMUNDANEDUPLICATE";
  readonly stepName: string;
  constructor(stepName: string) {
    super(`duplicate step name: ${stepName}`);
    this.name = "DuplicateStepError";
    this.stepName = stepName;
  }
}

export class StepFailedError extends Error {
  readonly code = "EMUNDANESTEPFAILED";
  readonly stepName: string;
  readonly original: unknown;
  constructor(stepName: string, original: unknown) {
    const msg =
      original instanceof Error
        ? `step ${JSON.stringify(stepName)} failed: ${original.message}`
        : `step ${JSON.stringify(stepName)} failed: ${String(original)}`;
    super(msg);
    this.name = "StepFailedError";
    this.stepName = stepName;
    this.original = original;
    if (original instanceof Error && original.stack) {
      this.stack = `${this.stack}\nCaused by: ${original.stack}`;
    }
  }
}
