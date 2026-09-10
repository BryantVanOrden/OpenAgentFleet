"""Run one accessibility query in a throwaway process and print the result.

AT-SPI can wedge: on 2026-09-09, with Firefox open on a long page, every call
into ``pyatspi.Registry`` blocked for good, and agentd -- which made that call
inline, holding the GIL -- stopped answering anything, health included. A
restart of agentd hung again on its first observe. The registry, not agentd,
was the thing that was stuck.

So the server process never touches pyatspi. ``a11y`` runs each query here,
in a child with a hard timeout; if the child does not answer, the query
returns nothing and agentd keeps serving. Usage::

    python a11y_child.py tree
    python a11y_child.py at_point '[640, 400]'
    python a11y_child.py window_title_at '[640, 400]'
"""

from __future__ import annotations

import json
import os
import sys

os.environ["AGENTD_A11Y_INPROC"] = "1"

import a11y  # noqa: E402  (after the env flag, so it runs in-process here)


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print("usage: a11y_child.py <tree|at_point|window_title_at> [json-args]", file=sys.stderr)
        return 2
    call = argv[1]
    args = json.loads(argv[2]) if len(argv) > 2 else []
    if call == "tree":
        result = [n.to_dict() for n in a11y.tree()]
    elif call == "at_point":
        node = a11y.at_point(*args)
        result = node.to_dict() if node else None
    elif call == "window_title_at":
        result = a11y.window_title_at(*args)
    else:
        print(f"unknown call {call!r}", file=sys.stderr)
        return 2
    sys.stdout.write(json.dumps(result))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
