"""The accessibility calls main.py makes must exist.

main.py cannot be imported here -- fastapi and mss are not installed in the
test environment -- so its calls are read out of the source instead. That is
enough for the question being asked, which is whether the names line up.
"""

import ast
import os
import unittest

import a11y

HERE = os.path.dirname(os.path.abspath(__file__))


def a11y_attributes_used_by(filename):
    """Every `a11y.<name>` referenced in a source file."""
    with open(os.path.join(HERE, filename), encoding="utf-8") as f:
        parsed = ast.parse(f.read())
    return {
        node.attr
        for node in ast.walk(parsed)
        if isinstance(node, ast.Attribute)
        and isinstance(node.value, ast.Name)
        and node.value.id == "a11y"
    }


class TestAccessibilityContract(unittest.TestCase):
    def test_every_accessibility_call_the_daemon_makes_resolves(self):
        """Set-of-Marks called a11y.snapshot(), which has never existed. The
        AttributeError was caught by the bare handler around the whole block,
        so every annotated observation came back with no marks and an
        unannotated screenshot, and the feature read as enabled throughout."""
        used = a11y_attributes_used_by("main.py")
        self.assertTrue(used, "no a11y calls found; the parser is looking in the wrong place")
        missing = sorted(name for name in used if not hasattr(a11y, name))
        self.assertEqual(missing, [], f"main.py calls a11y.{missing} which does not exist")

    def test_the_node_list_is_flat_so_set_of_marks_sees_widgets_not_windows(self):
        """tree() yields one root per application. Handing those straight to
        the annotator would mark whole windows and nothing inside them."""
        leaf = a11y.Node(role="push button", name="Submit", depth=2)
        middle = a11y.Node(role="panel", name="", depth=1, children=[leaf])
        root = a11y.Node(role="frame", name="Main", depth=0, children=[middle])

        original_tree, original_available = a11y.tree, a11y.AVAILABLE
        a11y.tree, a11y.AVAILABLE = (lambda: [root]), True
        try:
            flat = a11y.nodes()
        finally:
            a11y.tree, a11y.AVAILABLE = original_tree, original_available

        self.assertEqual([n.role for n in flat], ["frame", "panel", "push button"])

    def test_no_accessibility_bus_yields_an_empty_list_rather_than_an_error(self):
        original = a11y.AVAILABLE
        a11y.AVAILABLE = False
        try:
            self.assertEqual(a11y.nodes(), [])
        finally:
            a11y.AVAILABLE = original


if __name__ == "__main__":
    unittest.main()
