"""Screenshots are taken in a child with a hard timeout, so a wedged X server
costs one observe (answered at once with 503) and never the daemon."""

import subprocess
import tempfile
import time
import unittest
from unittest import mock

import capture


class CaptureInChildTests(unittest.TestCase):
    def setUp(self):
        self._inproc = capture.INPROC
        self._timeout = capture.CHILD_TIMEOUT
        self._flag = capture._unresponsive_since
        capture.INPROC = False

    def tearDown(self):
        capture.INPROC = self._inproc
        capture.CHILD_TIMEOUT = self._timeout
        capture._unresponsive_since = self._flag
        if capture._grab_lock.locked():
            capture._grab_lock.release()

    def test_a_stuck_display_raises_at_once_instead_of_hanging(self):
        capture.CHILD_TIMEOUT = 0.5

        def stuck(cmd, **kw):
            raise subprocess.TimeoutExpired(cmd, kw.get("timeout", 0))

        with mock.patch.object(subprocess, "run", stuck):
            with self.assertRaises(capture.DisplayUnresponsive):
                capture.grab()
        self.assertTrue(capture.display_unresponsive())

    def test_a_second_capture_waits_then_gives_up(self):
        capture.CHILD_TIMEOUT = 0.3
        self.assertTrue(capture._grab_lock.acquire(blocking=False))
        t0 = time.monotonic()
        with self.assertRaises(capture.DisplayUnresponsive) as err:
            capture.grab()
        self.assertGreaterEqual(time.monotonic() - t0, 0.25, "it should have waited for the capture in flight")
        self.assertIn("waiting on the display", str(err.exception))
        capture._grab_lock.release()

    def test_a_real_frame_clears_the_flag(self):
        from PIL import Image

        capture._unresponsive_since = 1.0

        def fake_child(cmd, **kw):
            Image.new("RGB", (64, 32), (10, 20, 30)).save(cmd[-1])
            return mock.Mock(returncode=0, stderr="", stdout="")

        with mock.patch.object(subprocess, "run", fake_child):
            frame = capture.grab()
        self.assertEqual((frame.width, frame.height), (64, 32))
        self.assertEqual(frame.image.getpixel((0, 0)), (10, 20, 30))
        self.assertFalse(capture.display_unresponsive())


if __name__ == "__main__":
    unittest.main()
