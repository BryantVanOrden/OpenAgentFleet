"""Set-of-Marks (SoM) visual annotation engine.

Overlays high-contrast, numbered bounding badges over interactive UI elements
derived from AT-SPI accessibility tree nodes. Enables vision models to target
elements by discrete Mark ID rather than guessing continuous pixel coordinates.
"""

from __future__ import annotations

from typing import Any
from PIL import Image, ImageDraw, ImageFont


# Role color palette (RGBA)
ROLE_COLORS: dict[str, tuple[int, int, int]] = {
    "push button": (46, 204, 113),       # Emerald green
    "toggle button": (46, 204, 113),
    "entry": (52, 152, 219),             # Bright blue
    "password text": (52, 152, 219),
    "text": (52, 152, 219),
    "link": (241, 196, 15),              # Gold yellow
    "menu item": (230, 126, 34),         # Orange
    "combo box": (155, 89, 182),         # Purple
    "check box": (26, 188, 156),         # Turquoise
    "radio button": (26, 188, 156),
    "tab": (231, 76, 60),                # Coral red
    "page tab": (231, 76, 60),
    "default": (149, 165, 166),          # Slate grey
}


def annotate_frame(
    image: Image.Image,
    nodes: list[Any],
    max_marks: int = 80,
    origin: tuple[int, int] = (0, 0),
) -> tuple[Image.Image, list[dict[str, Any]]]:
    """Draw Set-of-Marks badges on a frame.

    Two coordinate spaces meet here, and confusing them puts every click in the
    wrong place:

    * AT-SPI reports node extents in DESKTOP coordinates (pyatspi.DESKTOP_COORDS).
    * `image` is the captured frame, whose top-left is `origin` on the desktop.
      That is (0, 0) for a full-desktop grab and non-zero for a zoomed crop.

    So boxes are DRAWN at `node - origin` (frame space), while the returned
    `cx`/`cy` stay in DESKTOP space. The orchestrator therefore uses a mark
    centre verbatim and must not run it through the image->desktop mapping it
    applies to model-supplied coordinates — see `MarkItem` in pkg/protocol.

    Returns:
        (annotated_image, marks_list)
    """
    if not nodes:
        return image, []

    annotated = image.copy()
    draw = ImageDraw.Draw(annotated, "RGBA")
    marks: list[dict[str, Any]] = []

    mark_id = 1
    font = ImageFont.load_default()

    # Filter for nodes that have positive dimension and are on-screen
    img_w, img_h = image.size

    for node in nodes:
        if mark_id > max_marks:
            break

        # Desktop-space extents, kept for the returned centre.
        dx = getattr(node, "x", 0)
        dy = getattr(node, "y", 0)
        w = getattr(node, "w", 0)
        h = getattr(node, "h", 0)
        # Frame-space position, used for drawing.
        x = dx - origin[0]
        y = dy - origin[1]
        role = getattr(node, "role", "element")
        name = getattr(node, "name", "") or getattr(node, "text", "")

        # Skip degenerate, invisible, or fullscreen container geometries
        if w < 6 or h < 6 or w >= img_w * 0.95 or h >= img_h * 0.95:
            continue
        if x < 0 or y < 0 or x + w > img_w or y + h > img_h:
            continue

        color = ROLE_COLORS.get(role, ROLE_COLORS["default"])
        border_color = color + (220,)
        fill_color = color + (40,)

        # Draw semi-transparent element bounding box
        draw.rectangle([x, y, x + w, y + h], outline=border_color, fill=fill_color, width=2)

        # Draw badge pill at top-left of element
        badge_text = f" {mark_id} "
        # Approximate text bounding
        badge_w = len(badge_text) * 7 + 4
        badge_h = 14
        badge_x0 = max(0, x - 2)
        badge_y0 = max(0, y - badge_h + 2)
        badge_x1 = badge_x0 + badge_w
        badge_y1 = badge_y0 + badge_h

        draw.rectangle([badge_x0, badge_y0, badge_x1, badge_y1], fill=(20, 24, 33, 230), outline=border_color, width=1)
        draw.text((badge_x0 + 2, badge_y0 + 1), badge_text, fill=(255, 255, 255, 255), font=font)

        # Centre in DESKTOP space, so the orchestrator can click it directly.
        cx = dx + w // 2
        cy = dy + h // 2

        marks.append({
            "id": mark_id,
            "role": role,
            "label": name,
            "x": dx,
            "y": dy,
            "width": w,
            "height": h,
            "cx": cx,
            "cy": cy,
        })
        mark_id += 1

    return annotated, marks
