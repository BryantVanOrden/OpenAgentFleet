"""Coordinate-space tests for Set-of-Marks annotation.

`annotate_frame` straddles two coordinate systems and the whole point of the
`origin` argument is to keep them apart:

  * AT-SPI node extents arrive in DESKTOP space.
  * The image being annotated starts at `origin` on the desktop, so boxes must
    be DRAWN at `node - origin`.
  * The returned `cx`/`cy` must stay in DESKTOP space, because the orchestrator
    clicks a mark centre verbatim.

Getting either half wrong misplaces every click by exactly the crop offset, and
does so silently — the annotated picture still looks plausible. Hence these
tests check the returned numbers AND where ink actually landed.
"""

import unittest
from dataclasses import dataclass

from PIL import Image

from som import ROLE_COLORS, annotate_frame

BG = (0, 0, 0)


@dataclass
class Node:
    x: int
    y: int
    w: int
    h: int
    role: str = "push button"
    name: str = ""
    text: str = ""


def blank(w=400, h=300):
    return Image.new("RGB", (w, h), color=BG)


class TestOriginHandling(unittest.TestCase):
    def test_full_desktop_grab_centre_is_node_centre(self):
        _, marks = annotate_frame(blank(), [Node(x=100, y=80, w=60, h=40)])
        self.assertEqual(len(marks), 1)
        self.assertEqual(marks[0]["cx"], 130)
        self.assertEqual(marks[0]["cy"], 100)
        self.assertEqual((marks[0]["x"], marks[0]["y"]), (100, 80))

    def test_cropped_frame_returns_desktop_centre_not_frame_centre(self):
        # A 400x300 crop whose top-left sits at desktop (500, 200).
        origin = (500, 200)
        node = Node(x=560, y=240, w=80, h=40)
        _, marks = annotate_frame(blank(), [node], origin=origin)

        self.assertEqual(len(marks), 1)
        # Desktop space: 560 + 40, 240 + 20.
        self.assertEqual(marks[0]["cx"], 600)
        self.assertEqual(marks[0]["cy"], 260)
        # Frame space would have been (100, 60). If this ever matches, the
        # origin subtraction has leaked into the returned centre and every
        # mark click lands `origin` pixels off.
        self.assertNotEqual((marks[0]["cx"], marks[0]["cy"]), (100, 60))

    def test_reported_extents_stay_in_desktop_space(self):
        origin = (500, 200)
        _, marks = annotate_frame(
            blank(), [Node(x=560, y=240, w=80, h=40)], origin=origin
        )
        self.assertEqual(marks[0]["x"], 560)
        self.assertEqual(marks[0]["y"], 240)
        self.assertEqual(marks[0]["width"], 80)
        self.assertEqual(marks[0]["height"], 40)

    def test_centre_is_consistent_with_reported_extents(self):
        origin = (37, 11)
        nodes = [
            Node(x=137, y=111, w=41, h=27),   # odd dimensions exercise floor //
            Node(x=200, y=150, w=64, h=32),
        ]
        _, marks = annotate_frame(blank(), nodes, origin=origin)
        for m in marks:
            self.assertEqual(m["cx"], m["x"] + m["width"] // 2)
            self.assertEqual(m["cy"], m["y"] + m["height"] // 2)

    def test_origin_defaults_to_zero(self):
        _, a = annotate_frame(blank(), [Node(x=50, y=50, w=40, h=20)])
        _, b = annotate_frame(blank(), [Node(x=50, y=50, w=40, h=20)], origin=(0, 0))
        self.assertEqual(a, b)


class TestDrawingHappensInFrameSpace(unittest.TestCase):
    """The box must be painted at `node - origin`, not at the desktop coords."""

    def test_ink_lands_at_node_minus_origin(self):
        origin = (100, 50)
        node = Node(x=200, y=150, w=80, h=40)  # frame space: (100, 100) 80x40
        annotated, marks = annotate_frame(blank(), [node], origin=origin)
        self.assertEqual(len(marks), 1)

        # Inside the frame-space box: translucent fill over black background.
        self.assertNotEqual(annotated.getpixel((120, 115)), BG, "box not drawn at node - origin")
        # The desktop-space position is well outside that box and must be clean.
        # If drawing ignored `origin`, the box would sit at (200,150)-(280,190)
        # and this pixel would be inked instead.
        self.assertEqual(annotated.getpixel((240, 170)), BG, "box drawn at desktop coords, ignoring origin")

    def test_source_image_is_not_mutated(self):
        src = blank()
        annotated, _ = annotate_frame(src, [Node(x=50, y=50, w=60, h=30)])
        self.assertIsNot(annotated, src)
        self.assertEqual(src.getpixel((60, 60)), BG)
        self.assertNotEqual(annotated.getpixel((60, 60)), BG)


class TestVisibilityFiltering(unittest.TestCase):
    def test_node_left_of_the_crop_is_dropped(self):
        # Desktop x=100 with origin 500 => frame x=-400.
        _, marks = annotate_frame(
            blank(), [Node(x=100, y=240, w=60, h=30)], origin=(500, 200)
        )
        self.assertEqual(marks, [])

    def test_node_past_the_right_edge_is_dropped(self):
        # frame x = 380, w = 60 => 440 > 400.
        _, marks = annotate_frame(
            blank(), [Node(x=880, y=240, w=60, h=30)], origin=(500, 200)
        )
        self.assertEqual(marks, [])

    def test_node_exactly_at_the_origin_is_kept(self):
        _, marks = annotate_frame(
            blank(), [Node(x=500, y=200, w=60, h=30)], origin=(500, 200)
        )
        self.assertEqual(len(marks), 1)
        self.assertEqual((marks[0]["x"], marks[0]["y"]), (500, 200))

    def test_node_flush_with_the_far_edge_is_kept(self):
        # frame box ends exactly at (400, 300); the check is `>` not `>=`.
        _, marks = annotate_frame(
            blank(), [Node(x=840, y=470, w=60, h=30)], origin=(500, 200)
        )
        self.assertEqual(len(marks), 1)

    def test_degenerate_sizes_are_dropped(self):
        nodes = [
            Node(x=10, y=10, w=5, h=20),
            Node(x=10, y=40, w=20, h=5),
            Node(x=10, y=70, w=0, h=0),
            Node(x=10, y=100, w=6, h=6),  # exactly at the threshold: kept
        ]
        _, marks = annotate_frame(blank(), nodes)
        self.assertEqual(len(marks), 1)
        self.assertEqual((marks[0]["x"], marks[0]["y"]), (10, 100))

    def test_fullscreen_container_is_dropped(self):
        # 0.95 * 400 = 380, 0.95 * 300 = 285.
        _, marks = annotate_frame(blank(), [Node(x=0, y=0, w=390, h=290)])
        self.assertEqual(marks, [])

    def test_size_threshold_is_relative_to_the_image_not_the_desktop(self):
        """Documents a real consequence of cropping, not a bug in the maths.

        The 95% "fullscreen container" filter compares node size against the
        *image* size. In a tight crop a perfectly ordinary widget can exceed
        95% of the crop and vanish from the marks. Callers that crop small
        should expect fewer marks, not assume the element was missing from the
        accessibility tree.
        """
        node = Node(x=500, y=200, w=390, h=40)
        # In a 400px-wide crop the 390px node is filtered out ...
        _, cropped_marks = annotate_frame(blank(400, 300), [node], origin=(500, 200))
        self.assertEqual(cropped_marks, [])
        # ... but in a 1920px full-desktop frame it is kept.
        _, full_marks = annotate_frame(blank(1920, 1080), [node], origin=(0, 0))
        self.assertEqual(len(full_marks), 1)


class TestMarkNumbering(unittest.TestCase):
    def test_ids_are_dense_and_skip_filtered_nodes(self):
        nodes = [
            Node(x=10, y=10, w=2, h=2),                  # dropped (degenerate)
            Node(x=10, y=30, w=60, h=20, name="first"),
            Node(x=-500, y=60, w=60, h=20),              # dropped (off-screen)
            Node(x=10, y=90, w=60, h=20, name="second"),
        ]
        _, marks = annotate_frame(blank(), nodes)
        self.assertEqual([m["id"] for m in marks], [1, 2])
        self.assertEqual([m["label"] for m in marks], ["first", "second"])

    def test_max_marks_is_honoured(self):
        nodes = [Node(x=10, y=10 + i * 12, w=60, h=10) for i in range(20)]
        _, marks = annotate_frame(blank(), nodes, max_marks=5)
        self.assertEqual(len(marks), 5)
        self.assertEqual(marks[-1]["id"], 5)

    def test_label_prefers_name_then_text(self):
        nodes = [
            Node(x=10, y=10, w=60, h=20, name="Name wins", text="ignored"),
            Node(x=10, y=40, w=60, h=20, name="", text="text fallback"),
            Node(x=10, y=70, w=60, h=20, name="", text=""),
        ]
        _, marks = annotate_frame(blank(), nodes)
        self.assertEqual(
            [m["label"] for m in marks], ["Name wins", "text fallback", ""]
        )

    def test_unknown_role_uses_the_default_colour(self):
        _, marks = annotate_frame(
            blank(), [Node(x=10, y=10, w=60, h=20, role="sponge")]
        )
        self.assertEqual(marks[0]["role"], "sponge")
        self.assertNotIn("sponge", ROLE_COLORS)

    def test_empty_node_list_returns_the_original_image(self):
        src = blank()
        annotated, marks = annotate_frame(src, [])
        self.assertEqual(marks, [])
        self.assertIs(annotated, src)


class TestMissingAttributesAreTolerated(unittest.TestCase):
    def test_object_without_geometry_is_skipped_not_crashed(self):
        class Bare:
            role = "push button"

        _, marks = annotate_frame(blank(), [Bare(), Node(x=10, y=10, w=60, h=20)])
        self.assertEqual(len(marks), 1)


if __name__ == "__main__":
    unittest.main()
