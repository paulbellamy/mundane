"""mundane v1 — tiny durable-execution library.

One workflow run is one SQLite file. Crash, re-invoke, resume.
"""

from .core import (
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
]

__version__ = "1.0.0"
