"""open_url is one action for what used to be a twenty-step address-bar dance.
It drives the browser window that is already open, opens only things a browser
should open, and launches a browser only when none is there."""

import subprocess
import unittest
from unittest import mock

import inject


class OpenURLTests(unittest.TestCase):
    def setUp(self):
        self.calls = []
        self.windows = ["0x2400003"]
        self.popen = mock.patch.object(subprocess, "Popen").start()
        mock.patch.object(inject.time, "sleep").start()
        mock.patch.object(inject.shutil, "which", lambda name: "/usr/bin/firefox" if name == "firefox" else None).start()
        mock.patch.object(inject, "_run", self.fake_run).start()

    def tearDown(self):
        mock.patch.stopall()

    def fake_run(self, args, timeout=10):
        self.calls.append(args)
        if args[:2] == ["xdotool", "search"]:
            if "--class" in args:
                return 0, "\n".join(self.windows)
            return 1, ""  # no "Close Firefox" dialogs
        if args[:2] == ["xdotool", "getwindowname"]:
            return 0, "Fleet Notes — Mozilla Firefox"
        if args[:2] == ["xdotool", "getactivewindow"]:
            return 0, "Fleet Tasks — Mozilla Firefox"
        return 0, ""

    def test_an_address_goes_into_the_open_browser_window(self):
        ok, detail = inject.open_url("http://af-2f74f6ae-0bd:8001/tests.html")
        self.assertTrue(ok, detail)
        self.popen.assert_not_called()
        flat = [" ".join(c) for c in self.calls]
        self.assertTrue(any(c.startswith("xdotool windowactivate --sync 0x2400003") for c in flat), flat)
        self.assertIn("xdotool key --clearmodifiers ctrl+l", flat)
        self.assertIn("xdotool type --clearmodifiers --delay 12 -- http://af-2f74f6ae-0bd:8001/tests.html", flat)
        self.assertIn("xdotool key --clearmodifiers Return", flat)
        self.assertIn("Fleet Tasks", detail)

    def test_a_bare_host_gets_http(self):
        ok, _ = inject.open_url("af-2f74f6ae-0bd:8001")
        self.assertTrue(ok)
        typed = [c for c in self.calls if c[:2] == ["xdotool", "type"]][0]
        self.assertEqual(typed[-1], "http://af-2f74f6ae-0bd:8001")

    def test_only_web_and_file_addresses_are_opened(self):
        for bad in ("javascript:alert(1)", "ftp://x/y", "data:text/html,hi", ""):
            ok, detail = inject.open_url(bad)
            self.assertFalse(ok, bad)
        self.assertFalse(any(c[:2] == ["xdotool", "type"] for c in self.calls))
        self.popen.assert_not_called()

    def test_with_no_browser_window_one_is_launched_not_a_second_instance(self):
        self.windows = []
        ok, detail = inject.open_url("http://example.test")
        self.assertTrue(ok, detail)
        args = self.popen.call_args.args[0]
        self.assertEqual(args, ["/usr/bin/firefox", "http://example.test"])
        self.assertTrue(self.popen.call_args.kwargs["start_new_session"], "the browser must outlive the request")

    def test_no_browser_is_reported_not_raised(self):
        self.windows = []
        with mock.patch.object(inject.shutil, "which", lambda name: None):
            ok, detail = inject.open_url("http://example.test")
        self.assertFalse(ok)
        self.assertIn("no browser", detail)


if __name__ == "__main__":
    unittest.main()
