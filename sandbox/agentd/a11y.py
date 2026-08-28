"""AT-SPI accessibility bridge.

This is the difference between an agent that clicks coordinates and one that
clicks *the Build button*. Coordinates break the moment a window moves, a theme
changes, or a resolution differs from the recording; accessible labels survive
all three.

Everything here degrades to empty rather than raising: a desktop where AT-SPI is
unavailable should reduce the agent to vision-only, not take it down.
"""

from __future__ import annotations

import functools
from dataclasses import dataclass, field

try:
    import pyatspi

    AVAILABLE = True
except Exception:  # pragma: no cover - depends on the runtime desktop
    pyatspi = None
    AVAILABLE = False

# Roles worth showing a model. A full tree is mostly layout containers and would
# swamp the context window.
INTERESTING_ROLES = {
    "push button", "toggle button", "radio button", "check box", "combo box",
    "menu item", "check menu item", "radio menu item", "menu", "link",
    "text", "entry", "password text", "spin button", "slider",
    "list item", "table cell", "tab", "page tab", "tree item",
    "label", "heading", "dialog", "alert", "frame", "window",
}

MAX_NODES = 400
MAX_DEPTH = 14


@dataclass
class Node:
    role: str
    name: str
    depth: int
    x: int = 0
    y: int = 0
    w: int = 0
    h: int = 0
    enabled: bool = True
    focused: bool = False
    text: str = ""
    children: list["Node"] = field(default_factory=list)

    @property
    def center(self) -> tuple[int, int]:
        return self.x + self.w // 2, self.y + self.h // 2


def _extents(acc):
    try:
        component = acc.queryComponent()
        ext = component.getExtents(pyatspi.DESKTOP_COORDS)
        return int(ext.x), int(ext.y), int(ext.width), int(ext.height)
    except Exception:
        return 0, 0, 0, 0


def _state(acc, name: str) -> bool:
    try:
        return acc.getState().contains(getattr(pyatspi.STATE, name))
    except Exception:
        return False


def _text_of(acc) -> str:
    try:
        t = acc.queryText()
        return (t.getText(0, min(t.characterCount, 200)) or "").strip()
    except Exception:
        return ""


def _walk(acc, depth: int, budget: list[int]) -> Node | None:
    if budget[0] <= 0 or depth > MAX_DEPTH:
        return None
    try:
        role = acc.getRoleName()
        name = (acc.name or "").strip()
    except Exception:
        return None

    x, y, w, h = _extents(acc)
    # Zero-size and offscreen nodes cannot be clicked, so they are noise.
    visible = w > 0 and h > 0

    node = Node(
        role=role,
        name=name,
        depth=depth,
        x=x, y=y, w=w, h=h,
        enabled=_state(acc, "ENABLED"),
        focused=_state(acc, "FOCUSED"),
        text=_text_of(acc) if role in ("text", "entry", "password text", "label") else "",
    )
    budget[0] -= 1

    try:
        child_count = acc.childCount
    except Exception:
        child_count = 0
    for i in range(min(child_count, 60)):
        try:
            child = acc.getChildAtIndex(i)
        except Exception:
            continue
        if child is None:
            continue
        sub = _walk(child, depth + 1, budget)
        if sub is not None:
            node.children.append(sub)

    keep = visible and (role in INTERESTING_ROLES) and (name or node.text or node.children)
    if not keep and not node.children:
        return None
    return node


def tree() -> list[Node]:
    """Accessible tree for every application on the desktop."""
    if not AVAILABLE:
        return []
    budget = [MAX_NODES]
    roots: list[Node] = []
    try:
        desktop = pyatspi.Registry.getDesktop(0)
    except Exception:
        return []
    for i in range(desktop.childCount):
        try:
            app = desktop.getChildAtIndex(i)
        except Exception:
            continue
        if app is None:
            continue
        node = _walk(app, 0, budget)
        if node is not None:
            roots.append(node)
        if budget[0] <= 0:
            break
    return roots


def flatten(nodes: list[Node] | None = None) -> str:
    """Indented text rendering of the tree, sized for a prompt."""
    if nodes is None:
        nodes = tree()
    lines: list[str] = []

    def render(n: Node, indent: int) -> None:
        label = n.name or n.text
        if label or n.role in ("dialog", "alert", "frame"):
            marker = "*" if n.focused else "-"
            state = "" if n.enabled else " [disabled]"
            lines.append(
                f"{'  ' * indent}{marker} {n.role}: {label[:80]}{state} @{n.center[0]},{n.center[1]}"
            )
            indent += 1
        for c in n.children:
            render(c, indent)

    for root in nodes:
        render(root, 0)
    return "\n".join(lines[:MAX_NODES])


def flatten_text() -> str:
    """All visible label/text content, for wait_for and assert matching."""
    if not AVAILABLE:
        return ""
    chunks: list[str] = []

    def collect(n: Node) -> None:
        if n.name:
            chunks.append(n.name)
        if n.text:
            chunks.append(n.text)
        for c in n.children:
            collect(c)

    for root in tree():
        collect(root)
    return "\n".join(chunks)


def find(label: str, role: str | None = None) -> Node | None:
    """Locate a node by accessible name.

    Matching is deliberately forgiving — exact, then case-insensitive, then
    substring — because models paraphrase labels ("Save" for "Save As...").
    """
    if not AVAILABLE or not label:
        return None
    candidates: list[Node] = []

    def collect(n: Node) -> None:
        if n.w > 0 and n.h > 0 and n.enabled:
            candidates.append(n)
        for c in n.children:
            collect(c)

    for root in tree():
        collect(root)

    if role:
        role_matches = [n for n in candidates if n.role == role]
        if role_matches:
            candidates = role_matches

    target = label.strip()
    lowered = target.lower()

    for n in candidates:
        if n.name == target:
            return n
    for n in candidates:
        if n.name.lower() == lowered:
            return n
    for n in candidates:
        if lowered in n.name.lower() and n.name:
            return n
    for n in candidates:
        if n.name and n.name.lower() in lowered:
            return n
    return None


def at_point(x: int, y: int) -> Node | None:
    """Deepest accessible element under a screen coordinate.

    Used by the recorder to answer "what did the human actually click?" — the
    single most valuable thing a demonstration can capture.
    """
    if not AVAILABLE:
        return None
    try:
        desktop = pyatspi.Registry.getDesktop(0)
    except Exception:
        return None

    for i in range(desktop.childCount):
        try:
            app = desktop.getChildAtIndex(i)
            if app is None:
                continue
            hit = _descend_to_point(app, x, y, 0)
            if hit is not None:
                return hit
        except Exception:
            continue
    return None


def _descend_to_point(acc, x: int, y: int, depth: int) -> Node | None:
    if depth > MAX_DEPTH:
        return None
    try:
        component = acc.queryComponent()
        child = component.getAccessibleAtPoint(x, y, pyatspi.DESKTOP_COORDS)
    except Exception:
        child = None

    if child is not None:
        deeper = _descend_to_point(child, x, y, depth + 1)
        if deeper is not None:
            return deeper
        cx, cy, cw, ch = _extents(child)
        try:
            return Node(role=child.getRoleName(), name=(child.name or "").strip(),
                        depth=depth + 1, x=cx, y=cy, w=cw, h=ch)
        except Exception:
            return None

    # No child claims the point; report this node if it contains it.
    ax, ay, aw, ah = _extents(acc)
    if aw > 0 and ah > 0 and ax <= x < ax + aw and ay <= y < ay + ah:
        try:
            return Node(role=acc.getRoleName(), name=(acc.name or "").strip(),
                        depth=depth, x=ax, y=ay, w=aw, h=ah)
        except Exception:
            return None
    return None


def window_title_at(x: int, y: int) -> str:
    """Title of the top-level window containing a point."""
    if not AVAILABLE:
        return ""
    try:
        desktop = pyatspi.Registry.getDesktop(0)
    except Exception:
        return ""
    for i in range(desktop.childCount):
        try:
            app = desktop.getChildAtIndex(i)
            if app is None:
                continue
            for j in range(app.childCount):
                win = app.getChildAtIndex(j)
                if win is None:
                    continue
                wx, wy, ww, wh = _extents(win)
                if ww > 0 and wh > 0 and wx <= x < wx + ww and wy <= y < wy + wh:
                    return (win.name or "").strip()
        except Exception:
            continue
    return ""


@functools.lru_cache(maxsize=1)
def status() -> dict:
    return {"available": AVAILABLE}
