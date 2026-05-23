"""mundane v1 — tiny durable-execution library.

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
from .inspect import get_result, status, steps

__all__ = [
    "run",
    "arun",
    "status",
    "steps",
    "get_result",
    "LockedError",
    "SerializationError",
    "SchemaError",
    "StepFailedError",
    "DuplicateStepError",
]

__version__ = "2.0.0"
