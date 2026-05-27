"""mundane — tiny durable-execution library.

One workflow run is one SQLite file. Crash, re-invoke, resume.
"""

from .core import (
    DuplicateStepError,
    LockedError,
    SchemaError,
    SerializationError,
    StepFailedError,
    arun,
    run,
)

__all__ = [
    "run",
    "arun",
    "LockedError",
    "SerializationError",
    "SchemaError",
    "StepFailedError",
    "DuplicateStepError",
]

__version__ = "0.0.3"
