"""Take one screenshot in a throwaway process and write it to a file.

The X server can stop answering (a server grab nobody released did it on
2026-09-09), and an X request that never returns cannot be interrupted from
Python. Made inline, that request took agentd with it: every observe blocked,
the worker threads filled up, and health stopped answering too. So the
screenshot is taken here, in a child the parent can time out and kill.

Usage: python capture_child.py <output.png>
"""

from __future__ import annotations

import sys

import mss
import mss.tools


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print("usage: capture_child.py <output.png>", file=sys.stderr)
        return 2
    with mss.mss() as sct:
        monitor = sct.monitors[1] if len(sct.monitors) > 1 else sct.monitors[0]
        raw = sct.grab(monitor)
        mss.tools.to_png(raw.rgb, raw.size, output=argv[1])
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
