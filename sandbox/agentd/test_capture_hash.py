"""Tests for capture.py's pure image maths: dHash, crop clamping, encode.

`capture` imports `mss`, which needs a real X display and is not installed in a
bare test container, so it is stubbed in `sys.modules` before import. Nothing
below calls `grab()`, `active_window()` or `screen_text()` — those genuinely
need a display, an `xdotool` binary and an AT-SPI bus respectively, and are
skipped rather than faked.
"""

import base64
import io
import sys
import types
import unittest

from PIL import Image

# mss is X-only; stub it so the rest of the module is importable.
sys.modules.setdefault("mss", types.ModuleType("mss"))

import capture  # noqa: E402
from capture import Frame  # noqa: E402

_MSS_STUBBED = not hasattr(sys.modules["mss"], "mss")


def grey(values):
    """Build a 9x8 greyscale image from an 8-row list of 9 values."""
    img = Image.new("L", (capture._HASH_W, capture._HASH_H))
    for y, row in enumerate(values):
        for x, v in enumerate(row):
            img.putpixel((x, y), v)
    return img


def frame_of(img):
    return Frame(image=img, width=img.width, height=img.height, scale=1.0)


class TestDHashBitMath(unittest.TestCase):
    """The dHash is the entire basis of stall detection.

    64 comparisons (8 rows x 8 adjacent pairs) must produce exactly 16 hex
    characters, and the bit for "this pixel is brighter than the one to its
    right" must be set — never the reverse, or "screen changed" and "screen
    frozen" swap meanings.
    """

    def test_hash_is_sixteen_hex_characters(self):
        h = capture.dhash(frame_of(grey([[128] * 9] * 8)))
        self.assertEqual(len(h), 16)
        int(h, 16)  # must parse as hex

    def test_uniform_image_sets_no_bits(self):
        self.assertEqual(capture.dhash(frame_of(grey([[128] * 9] * 8))), "0" * 16)

    def test_strictly_decreasing_rows_set_every_bit(self):
        rows = [[250 - x * 20 for x in range(9)] for _ in range(8)]
        self.assertEqual(capture.dhash(frame_of(grey(rows))), "f" * 16)

    def test_strictly_increasing_rows_set_no_bits(self):
        rows = [[10 + x * 20 for x in range(9)] for _ in range(8)]
        self.assertEqual(capture.dhash(frame_of(grey(rows))), "0" * 16)

    def test_bit_index_advances_row_major(self):
        # Only the very first comparison of row 0 is "brighter than the right
        # neighbour", so only bit 0 may be set.
        rows = [[0] * 9 for _ in range(8)]
        rows[0][0] = 200
        self.assertEqual(int(capture.dhash(frame_of(grey(rows))), 16), 1)

        # Only the last comparison of the last row => bit 63.
        rows = [[0] * 9 for _ in range(8)]
        rows[7][7] = 200
        self.assertEqual(int(capture.dhash(frame_of(grey(rows))), 16), 1 << 63)

    def test_ninth_column_is_only_a_comparison_partner(self):
        # Column 8 never gets its own bit; changing it only flips the bit for
        # the pair (7, 8).
        base = [[100] * 9 for _ in range(8)]
        h_low = capture.dhash(frame_of(grey(base)))
        bumped = [row[:] for row in base]
        bumped[3][8] = 10  # now pixel 7 > pixel 8 in row 3 => bit 3*8+7 = 31
        h_high = capture.dhash(frame_of(grey(bumped)))
        self.assertEqual(int(h_high, 16) ^ int(h_low, 16), 1 << 31)

    def test_hash_is_deterministic(self):
        rows = [[(x * 7 + y * 31) % 256 for x in range(9)] for y in range(8)]
        f = frame_of(grey(rows))
        self.assertEqual(capture.dhash(f), capture.dhash(f))

    def test_identical_content_hashes_alike_and_different_content_does_not(self):
        rows = [[(x * 7 + y * 31) % 256 for x in range(9)] for y in range(8)]
        a = capture.dhash(frame_of(grey(rows)))
        b = capture.dhash(frame_of(grey([r[:] for r in rows])))
        self.assertEqual(a, b)

        changed = [r[:] for r in rows]
        changed[4] = list(reversed(changed[4]))
        self.assertNotEqual(capture.dhash(frame_of(grey(changed))), a)

    def test_rgb_input_is_converted_to_luminance(self):
        # A pure-red and pure-blue frame differ in RGB but the hash operates on
        # luminance, so both are flat and both hash to zero.
        for colour in ((255, 0, 0), (0, 0, 255)):
            img = Image.new("RGB", (64, 48), color=colour)
            self.assertEqual(capture.dhash(frame_of(img)), "0" * 16)

    def test_large_frame_is_downsampled_before_hashing(self):
        # A big frame must still produce a 16-char hash (the resize is what
        # makes the hash insensitive to antialiasing noise).
        img = Image.new("RGB", (1920, 1080), color=(30, 30, 30))
        self.assertEqual(len(capture.dhash(frame_of(img))), 16)


class TestCropClamping(unittest.TestCase):
    """Crop feeds `origin` to som.annotate_frame, so its arithmetic matters."""

    def setUp(self):
        self.frame = frame_of(Image.new("RGB", (800, 600), color=(10, 10, 10)))

    def test_normal_crop_sets_size_and_origin(self):
        out = capture.crop(self.frame, (100, 50, 300, 200))
        self.assertEqual(out.origin, (100, 50))
        self.assertEqual((out.width, out.height), (200, 150))
        self.assertEqual(out.image.size, (200, 150))
        self.assertEqual(out.scale, 1.0)

    def test_negative_coordinates_clamp_to_zero(self):
        out = capture.crop(self.frame, (-40, -90, 120, 80))
        self.assertEqual(out.origin, (0, 0))
        self.assertEqual((out.width, out.height), (120, 80))

    def test_region_past_the_edge_clamps_to_the_frame(self):
        out = capture.crop(self.frame, (700, 500, 5000, 5000))
        self.assertEqual(out.origin, (700, 500))
        self.assertEqual((out.width, out.height), (100, 100))

    def test_inverted_region_yields_the_minimum_box_not_an_exception(self):
        out = capture.crop(self.frame, (400, 300, 100, 50))
        self.assertEqual(out.origin, (400, 300))
        self.assertEqual((out.width, out.height), (16, 16))

    def test_zero_area_region_yields_the_minimum_box(self):
        out = capture.crop(self.frame, (10, 10, 10, 10))
        self.assertEqual((out.width, out.height), (16, 16))

    def test_origin_at_the_far_corner_is_clamped_inside_the_frame(self):
        out = capture.crop(self.frame, (99999, 99999, 99999, 99999))
        self.assertEqual(out.origin, (799, 599))
        # x1 = max(799+16, min(99999, 800)) = 815, so the box runs past the
        # image edge; PIL pads rather than raising, which keeps a bad model
        # region from taking the daemon down.
        self.assertEqual((out.width, out.height), (16, 16))

    def test_float_region_is_truncated_to_ints(self):
        out = capture.crop(self.frame, (10.9, 20.9, 110.9, 120.9))
        self.assertEqual(out.origin, (10, 20))
        self.assertEqual((out.width, out.height), (100, 100))


class TestCropFeedsSomOrigin(unittest.TestCase):
    """The contract that ties the two modules together.

    A crop's `origin` is exactly what `annotate_frame` needs to keep mark
    centres in desktop space. This is the round trip a click actually takes.
    """

    def test_mark_centre_survives_the_crop_round_trip(self):
        from dataclasses import dataclass

        from som import annotate_frame

        @dataclass
        class Node:
            x: int
            y: int
            w: int
            h: int
            role: str = "push button"
            name: str = "OK"
            text: str = ""

        desktop = frame_of(Image.new("RGB", (1280, 800), color=(0, 0, 0)))
        cropped = capture.crop(desktop, (400, 300, 800, 600))

        # A button sitting at desktop (500, 350), 100x40.
        node = Node(x=500, y=350, w=100, h=40)
        _, marks = annotate_frame(cropped.image, [node], origin=cropped.origin)

        self.assertEqual(len(marks), 1)
        self.assertEqual((marks[0]["cx"], marks[0]["cy"]), (550, 370))
        # Sanity: that centre lies inside the crop when translated to frame
        # space, so the badge really was drawn on this picture.
        fx = marks[0]["cx"] - cropped.origin[0]
        fy = marks[0]["cy"] - cropped.origin[1]
        self.assertTrue(0 <= fx < cropped.width and 0 <= fy < cropped.height)


class TestEncode(unittest.TestCase):
    def test_downscale_reports_the_applied_scale(self):
        f = frame_of(Image.new("RGB", (1920, 1080), color=(20, 20, 20)))
        b64, scale = capture.encode(f, max_width=1280)
        self.assertAlmostEqual(scale, 1280 / 1920)
        decoded = Image.open(io.BytesIO(base64.b64decode(b64)))
        self.assertEqual(decoded.width, 1280)
        self.assertEqual(decoded.format, "WEBP")

    def test_small_frame_is_not_upscaled_and_scale_is_one(self):
        f = frame_of(Image.new("RGB", (640, 480), color=(20, 20, 20)))
        b64, scale = capture.encode(f, max_width=1280)
        self.assertEqual(scale, 1.0)
        decoded = Image.open(io.BytesIO(base64.b64decode(b64)))
        self.assertEqual(decoded.size, (640, 480))

    def test_aspect_ratio_is_preserved(self):
        f = frame_of(Image.new("RGB", (1600, 900), color=(20, 20, 20)))
        b64, _ = capture.encode(f, max_width=800)
        decoded = Image.open(io.BytesIO(base64.b64decode(b64)))
        self.assertEqual(decoded.size, (800, 450))

    def test_extremely_wide_thin_frame_keeps_at_least_one_pixel_of_height(self):
        f = frame_of(Image.new("RGB", (4000, 3), color=(20, 20, 20)))
        b64, _ = capture.encode(f, max_width=100)
        decoded = Image.open(io.BytesIO(base64.b64decode(b64)))
        self.assertGreaterEqual(decoded.height, 1)

    def test_output_is_valid_base64_ascii(self):
        f = frame_of(Image.new("RGB", (64, 64), color=(1, 2, 3)))
        b64, _ = capture.encode(f)
        b64.encode("ascii")
        self.assertTrue(base64.b64decode(b64).startswith(b"RIFF"))


class TestFrameDefaults(unittest.TestCase):
    def test_origin_defaults_to_zero(self):
        f = Frame(image=Image.new("RGB", (10, 10)), width=10, height=10, scale=1.0)
        self.assertEqual(f.origin, (0, 0))


@unittest.skipUnless(
    _MSS_STUBBED, "mss is genuinely installed; skip the stub-only guard"
)
class TestDisplayBoundFunctionsAreNotExercised(unittest.TestCase):
    """Named explicitly so the gap is visible rather than silently absent."""

    def test_grab_requires_a_real_display(self):
        self.assertTrue(callable(capture.grab))
        with self.assertRaises(AttributeError):
            capture.grab()  # the stubbed mss module has no `mss` attribute

    def test_screen_text_degrades_to_empty_without_atspi(self):
        # a11y sets AVAILABLE = False when pyatspi is missing and returns
        # empty rather than raising, so a desktop with no accessibility bus
        # reduces the agent to vision-only instead of taking it down.
        import a11y

        self.assertFalse(a11y.AVAILABLE)
        self.assertEqual(capture.screen_text(), "")


if __name__ == "__main__":
    unittest.main()
