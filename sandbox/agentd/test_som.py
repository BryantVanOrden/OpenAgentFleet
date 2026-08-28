"""Unit tests for Set-of-Marks visual annotation."""

import unittest
from dataclasses import dataclass
from PIL import Image
from som import annotate_frame, ROLE_COLORS


@dataclass
class DummyNode:
    x: int
    y: int
    w: int
    h: int
    role: str
    name: str = ""
    text: str = ""


class TestSetOfMarks(unittest.TestCase):
    def setUp(self):
        self.image = Image.new("RGB", (800, 600), color=(30, 30, 30))

    def test_empty_nodes(self):
        annotated, marks = annotate_frame(self.image, [])
        self.assertEqual(len(marks), 0)
        self.assertEqual(annotated.size, (800, 600))

    def test_annotate_valid_nodes(self):
        nodes = [
            DummyNode(x=50, y=50, w=120, h=40, role="push button", name="Submit"),
            DummyNode(x=50, y=120, w=200, h=35, role="entry", text="Search query"),
            DummyNode(x=50, y=200, w=150, h=25, role="link", name="Documentation"),
        ]
        annotated, marks = annotate_frame(self.image, nodes)
        self.assertEqual(len(marks), 3)

        # First mark
        self.assertEqual(marks[0]["id"], 1)
        self.assertEqual(marks[0]["role"], "push button")
        self.assertEqual(marks[0]["label"], "Submit")
        self.assertEqual(marks[0]["cx"], 50 + 60)
        self.assertEqual(marks[0]["cy"], 50 + 20)

        # Second mark
        self.assertEqual(marks[1]["id"], 2)
        self.assertEqual(marks[1]["role"], "entry")
        self.assertEqual(marks[1]["label"], "Search query")

        # Third mark
        self.assertEqual(marks[2]["id"], 3)
        self.assertEqual(marks[2]["role"], "link")
        self.assertEqual(marks[2]["label"], "Documentation")

    def test_skips_fullscreen_containers_and_degenerate(self):
        nodes = [
            DummyNode(x=0, y=0, w=790, h=590, role="frame", name="Main Window"),  # fullscreen container
            DummyNode(x=10, y=10, w=2, h=2, role="label", name="Tiny dot"),        # degenerate size < 6px
            DummyNode(x=-50, y=10, w=100, h=30, role="push button", name="Offscreen"),
            DummyNode(x=100, y=100, w=80, h=30, role="push button", name="Valid Button"),
        ]
        annotated, marks = annotate_frame(self.image, nodes)
        self.assertEqual(len(marks), 1)
        self.assertEqual(marks[0]["label"], "Valid Button")
        self.assertEqual(marks[0]["id"], 1)


if __name__ == "__main__":
    unittest.main()
