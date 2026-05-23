"""Parse human-friendly duration strings.

Accepts strings like "30s", "5m", "2h", "1d", "500ms", or ints/floats in
seconds (matching neither TS nor JS conventions strictly — the TS API
documents number-of-milliseconds, the Python API documents number-of-seconds).
For consistency with the TS interface, we accept both: strings and numbers
where numbers are treated as milliseconds when called from the public API
(see core.py).

This module only parses strings and returns milliseconds.
"""

import re

_UNIT_MS = {
    "ms": 1,
    "s": 1000,
    "m": 60 * 1000,
    "h": 60 * 60 * 1000,
    "d": 24 * 60 * 60 * 1000,
}

# Matches: "5m", "1h", "2.5s", "500ms". Allow integer or decimal magnitudes.
_RE = re.compile(r"^\s*(\d+(?:\.\d+)?)(ms|s|m|h|d)\s*$", re.IGNORECASE)


def parse_duration_ms(s: str) -> int:
    """Parse a duration string into integer milliseconds."""
    if not isinstance(s, str):
        raise TypeError(f"duration must be a string, got {type(s).__name__}")
    m = _RE.match(s)
    if not m:
        raise ValueError(
            f"invalid duration {s!r}: expected e.g. '500ms', '30s', '5m', '2h', '1d'"
        )
    magnitude = float(m.group(1))
    unit = m.group(2).lower()
    return int(round(magnitude * _UNIT_MS[unit]))
