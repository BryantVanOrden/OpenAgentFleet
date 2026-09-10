"""Screenshots are taken in a child with a hard timeout, so a wedged X server
costs one observe (answered at once with 503) and never the daemon."""

import subprocess

import pytest

import capture


def test_a_stuck_display_raises_at_once_instead_of_hanging(monkeypatch):
    monkeypatch.setattr(capture, "INPROC", False)
    monkeypatch.setattr(capture, "CHILD_TIMEOUT", 0.5)
    real_run = subprocess.run

    def stuck(cmd, **kw):
        raise subprocess.TimeoutExpired(cmd, kw.get("timeout", 0))

    monkeypatch.setattr(subprocess, "run", stuck)
    try:
        with pytest.raises(capture.DisplayUnresponsive):
            capture.grab()
        assert capture.display_unresponsive() is True
    finally:
        monkeypatch.setattr(subprocess, "run", real_run)


def test_a_second_capture_waits_then_gives_up(monkeypatch):
    monkeypatch.setattr(capture, "INPROC", False)
    monkeypatch.setattr(capture, "CHILD_TIMEOUT", 0.3)
    assert capture._grab_lock.acquire(blocking=False)
    try:
        import time
        t0 = time.monotonic()
        with pytest.raises(capture.DisplayUnresponsive) as err:
            capture.grab()
        assert time.monotonic() - t0 >= 0.25, "it should have waited for the capture in flight"
        assert "waiting on the display" in str(err.value)
    finally:
        capture._grab_lock.release()


def test_a_real_frame_clears_the_flag(monkeypatch, tmp_path):
    from PIL import Image

    monkeypatch.setattr(capture, "INPROC", False)
    capture._unresponsive_since = 1.0

    def fake_child(cmd, **kw):
        Image.new("RGB", (64, 32), (10, 20, 30)).save(cmd[-1])

        class Done:
            returncode = 0
            stderr = ""
            stdout = ""

        return Done()

    monkeypatch.setattr(subprocess, "run", fake_child)
    frame = capture.grab()
    assert (frame.width, frame.height) == (64, 32)
    assert frame.image.getpixel((0, 0)) == (10, 20, 30)
    assert capture.display_unresponsive() is False
