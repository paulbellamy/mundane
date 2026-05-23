"""LLM content pipeline that never pays for the same tokens twice.

Each model call is a mundane step. Kill the process mid-run (Ctrl-C, crash,
OOM) and re-invoke: completed calls return from the SQLite file, so you only
pay for the calls that didn't finish.

Offline + fakeable: the "model" is a local stub that prints a `$$` line every
time it actually runs. A resumed run makes *no* new calls for cached steps —
you can see exactly which calls you were spared.

    # First run, crash before drafting the "tradeoffs" section:
    CRASH_AT=tradeoffs python3 pipeline.py post.db

    # Re-invoke against the same file: outline + earlier sections are cached,
    # so it resumes mid-pipeline and only pays for what's left:
    python3 pipeline.py post.db

    # Inspect the durable state:
    ../../go/mundane-bin steps post.db
"""

import hashlib
import os
import sys
import time

import mundane

TOPIC = "durable execution with sqlite"
SECTIONS = ["intro", "how-it-works", "tradeoffs", "conclusion"]


def fake_llm(prompt: str) -> str:
    """Stand-in for a paid API call. Prints only when actually invoked."""
    print(f"  $$ model call: {prompt[:52]!r}", flush=True)
    time.sleep(0.4)  # pretend we waited on the network
    digest = hashlib.sha256(prompt.encode()).hexdigest()[:8]
    return f"[{digest}] {prompt}"


def workflow(ctx):
    outline = ctx.step("outline", lambda: fake_llm(f"outline a post about {TOPIC}"))

    crash_at = os.environ.get("CRASH_AT")
    drafts = []
    for section in SECTIONS:
        if crash_at == section:
            raise RuntimeError(f"simulated crash before drafting {section!r}")
        draft = ctx.step(
            f"draft-{section}",
            lambda section=section: fake_llm(
                f"write the {section} section, given outline {outline[:18]}"
            ),
        )
        drafts.append(draft)

    post = ctx.step("assemble", lambda: "\n\n".join(drafts))
    print(f"\n>> assembled post: {len(post)} chars across {len(drafts)} sections")
    return post


if __name__ == "__main__":
    db = sys.argv[1] if len(sys.argv) > 1 else "ai-pipeline.db"
    mundane.run(db, workflow)
