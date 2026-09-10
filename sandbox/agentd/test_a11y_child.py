"""The accessibility tree is queried in a child process with a hard timeout, so
a wedged AT-SPI registry costs one query and never the daemon."""

import json
import os
import subprocess
import sys
import unittest
from unittest import mock

import a11y


class A11yInChildTests(unittest.TestCase):
    def setUp(self):
        self._saved = (a11y.CHILD_TIMEOUT, a11y.AVAILABLE, a11y.INPROC, a11y._wedged_at)

    def tearDown(self):
        a11y.CHILD_TIMEOUT, a11y.AVAILABLE, a11y.INPROC, a11y._wedged_at = self._saved

    def test_node_round_trips_through_json(self):
        n = a11y.Node(role="frame", name="Fleet Notes", depth=0, x=1, y=2, w=300, h=200,
                      children=[a11y.Node(role="push button", name="Save", depth=1, x=5, y=6, w=40, h=20, focused=True)])
        back = a11y.Node.from_dict(json.loads(json.dumps(n.to_dict())))
        self.assertEqual(back, n)
        self.assertEqual(back.children[0].center, (25, 16))

    def test_child_answers_with_a_json_list(self):
        # Without a desktop the child reports an empty tree -- and exits, which
        # is the property that matters: the server never blocks on it.
        script = os.path.join(os.path.dirname(a11y.__file__), "a11y_child.py")
        proc = subprocess.run([sys.executable, script, "tree"], capture_output=True, text=True, timeout=30,
                              cwd=os.path.dirname(script))
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(json.loads(proc.stdout), [])

    def test_a_query_that_never_answers_is_dropped_not_waited_for(self):
        a11y.CHILD_TIMEOUT = 0.5
        a11y.AVAILABLE = True
        a11y.INPROC = False

        def slow_run(cmd, **kw):
            raise subprocess.TimeoutExpired(cmd, kw.get("timeout", 0))

        with mock.patch.object(subprocess, "run", slow_run):
            self.assertEqual(a11y.tree(), [])
            self.assertIsNone(a11y.at_point(1, 1))
            self.assertEqual(a11y.window_title_at(1, 1), "")
        self.assertTrue(a11y.wedged())
        self.assertTrue(a11y.status()["wedged"])

    def test_a_good_answer_clears_the_wedged_flag(self):
        a11y.AVAILABLE = True
        a11y.INPROC = False
        a11y._wedged_at = 1.0
        done = mock.Mock(returncode=0, stderr="",
                         stdout=json.dumps([a11y.Node(role="window", name="w", depth=0).to_dict()]))
        with mock.patch.object(subprocess, "run", lambda *a, **k: done):
            roots = a11y.tree()
        self.assertEqual([r.name for r in roots], ["w"])
        self.assertFalse(a11y.wedged())


if __name__ == "__main__":
    unittest.main()
