package protocol

import "testing"

// The coordinate helpers are the single most safety-critical piece of pure logic
// in the repo: a mistake here does not throw, it silently lands every click in
// the wrong place. The cases below pin the three spaces down explicitly —
// desktop pixels, frame pixels (the crop), and image pixels (what the model saw).

// ------------------------------------------------------ ImageWidth/Height ---

func TestObservationImageDimensions(t *testing.T) {
	cases := []struct {
		name                  string
		obs                   Observation
		wantWidth, wantHeight int
	}{
		{
			name: "downscaled full desktop reports the ENCODED size",
			// 1920x1080 desktop shrunk to a 1280-wide image for the model.
			obs:        Observation{Width: 1920, Height: 1080, Scale: 1280.0 / 1920.0, DesktopWidth: 1920, DesktopHeight: 1080},
			wantWidth:  1280,
			wantHeight: 720,
		},
		{
			name:       "scale 1 leaves the frame size alone",
			obs:        Observation{Width: 1280, Height: 800, Scale: 1},
			wantWidth:  1280,
			wantHeight: 800,
		},
		{
			name: "a crop reports the crop's encoded size, not the desktop's",
			obs: Observation{
				Width: 600, Height: 400, Scale: 0.5,
				OriginX: 800, OriginY: 300,
				DesktopWidth: 1920, DesktopHeight: 1080,
			},
			wantWidth:  300,
			wantHeight: 200,
		},
		{
			name:       "scale 0 falls back to the frame size rather than reporting 0x0",
			obs:        Observation{Width: 1024, Height: 768, Scale: 0},
			wantWidth:  1024,
			wantHeight: 768,
		},
		{
			name:       "a negative scale also falls back",
			obs:        Observation{Width: 1024, Height: 768, Scale: -2},
			wantWidth:  1024,
			wantHeight: 768,
		},
		{
			name:       "an upscaled frame is reported larger than the frame",
			obs:        Observation{Width: 640, Height: 480, Scale: 2},
			wantWidth:  1280,
			wantHeight: 960,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.obs
			if got := o.ImageWidth(); got != tc.wantWidth {
				t.Errorf("ImageWidth() = %d, want %d", got, tc.wantWidth)
			}
			if got := o.ImageHeight(); got != tc.wantHeight {
				t.Errorf("ImageHeight() = %d, want %d", got, tc.wantHeight)
			}
		})
	}
}

func TestImageDimensionsAreNotTheDesktopDimensions(t *testing.T) {
	// Regression guard for the failure mode called out in prompt.go: telling the
	// model the display is 1920 wide while handing it a 1280-wide picture.
	o := Observation{
		Width: 1920, Height: 1080, Scale: 0.5,
		DesktopWidth: 1920, DesktopHeight: 1080,
	}
	if o.ImageWidth() == o.DesktopWidth || o.ImageHeight() == o.DesktopHeight {
		t.Fatalf("ImageWidth/Height (%dx%d) leaked the desktop size (%dx%d)",
			o.ImageWidth(), o.ImageHeight(), o.DesktopWidth, o.DesktopHeight)
	}
	if o.ImageWidth() != 960 || o.ImageHeight() != 540 {
		t.Errorf("got %dx%d, want 960x540", o.ImageWidth(), o.ImageHeight())
	}
}

// ---------------------------------------------------------------- ToDesktop ---

func TestObservationToDesktop(t *testing.T) {
	// A 1920x1080 desktop captured whole and encoded at 2/3 size.
	fullDesktop := Observation{
		Width: 1920, Height: 1080, Scale: 1280.0 / 1920.0,
		OriginX: 0, OriginY: 0,
		DesktopWidth: 1920, DesktopHeight: 1080,
	}
	// A 1:1 crop of the right-hand half of that same desktop.
	crop := Observation{
		Width: 960, Height: 540, Scale: 1,
		OriginX: 960, OriginY: 540,
		DesktopWidth: 1920, DesktopHeight: 1080,
	}

	cases := []struct {
		name         string
		obs          Observation
		x, y         int
		wantX, wantY int
	}{
		// ---- full-desktop frame, scale < 1 ----
		{"full desktop origin maps to origin", fullDesktop, 0, 0, 0, 0},
		{"full desktop centre scales back up", fullDesktop, 640, 360, 960, 540},
		{"full desktop arbitrary point", fullDesktop, 320, 180, 480, 270},
		{
			// The far corner of a 1280x720 image is 1279,719 -> 1918,1078.
			"full desktop far corner stays inside the display",
			fullDesktop, 1279, 719, 1918, 1078,
		},

		// ---- cropped/zoomed frame, scale 1, non-zero origin ----
		{"crop origin offsets by OriginX/OriginY", crop, 0, 0, 960, 540},
		{"crop interior point offsets", crop, 100, 50, 1060, 590},
		{"crop far corner", crop, 959, 539, 1919, 1079},

		// ---- clamping ----
		{"x past the right edge clamps to DesktopWidth-1", fullDesktop, 99999, 100, 1919, 150},
		{"y past the bottom edge clamps to DesktopHeight-1", fullDesktop, 100, 99999, 150, 1079},
		{"negative coordinates clamp to zero", fullDesktop, -500, -500, 0, 0},
		{"a crop overshoot clamps to the desktop, not past it", crop, 5000, 5000, 1919, 1079},
		{"a crop undershoot clamps to zero, not to a negative", crop, -5000, -5000, 0, 0},

		// ---- degenerate scale must not divide by zero ----
		{
			"scale 0 is treated as 1:1",
			Observation{Width: 800, Height: 600, Scale: 0, DesktopWidth: 800, DesktopHeight: 600},
			400, 300, 400, 300,
		},
		{
			"a negative scale is treated as 1:1 rather than mirroring",
			Observation{Width: 800, Height: 600, Scale: -0.5, DesktopWidth: 800, DesktopHeight: 600},
			400, 300, 400, 300,
		},
		{
			"scale 0 on a crop still applies the origin",
			Observation{Width: 400, Height: 300, Scale: 0, OriginX: 100, OriginY: 200, DesktopWidth: 1920, DesktopHeight: 1080},
			10, 10, 110, 210,
		},

		// ---- missing desktop dimensions fall back to origin+frame ----
		{
			"no desktop size falls back to the frame bounds",
			Observation{Width: 640, Height: 480, Scale: 1},
			9999, 9999, 639, 479,
		},
		{
			"no desktop size on a crop bounds at origin+frame",
			Observation{Width: 640, Height: 480, Scale: 1, OriginX: 100, OriginY: 100},
			9999, 9999, 739, 579,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.obs
			gotX, gotY := o.ToDesktop(tc.x, tc.y)
			if gotX != tc.wantX || gotY != tc.wantY {
				t.Errorf("ToDesktop(%d,%d) = (%d,%d), want (%d,%d)",
					tc.x, tc.y, gotX, gotY, tc.wantX, tc.wantY)
			}
		})
	}
}

func TestToDesktopNeverEscapesTheDisplay(t *testing.T) {
	// Whatever the model says, the result has to be a coordinate the display
	// actually has. Anything else is an input event sent into the void.
	obs := Observation{
		Width: 1920, Height: 1080, Scale: 0.5,
		DesktopWidth: 1920, DesktopHeight: 1080,
	}
	inputs := []int{-1 << 30, -1, 0, 1, 959, 960, 5000, 1 << 30}
	for _, x := range inputs {
		for _, y := range inputs {
			gx, gy := obs.ToDesktop(x, y)
			if gx < 0 || gx >= obs.DesktopWidth {
				t.Errorf("ToDesktop(%d,%d) x = %d, outside [0,%d)", x, y, gx, obs.DesktopWidth)
			}
			if gy < 0 || gy >= obs.DesktopHeight {
				t.Errorf("ToDesktop(%d,%d) y = %d, outside [0,%d)", x, y, gy, obs.DesktopHeight)
			}
		}
	}
}

func TestToDesktopIsTheInverseOfScaling(t *testing.T) {
	// Round-trip: take a desktop point, project it into image space the way the
	// encoder would, map it back, and it should land within a pixel of itself.
	obs := Observation{
		Width: 1600, Height: 1200, Scale: 0.4,
		DesktopWidth: 1600, DesktopHeight: 1200,
	}
	for _, want := range [][2]int{{0, 0}, {100, 100}, {800, 600}, {1500, 1100}} {
		imgX := int(float64(want[0]) * obs.Scale)
		imgY := int(float64(want[1]) * obs.Scale)
		gotX, gotY := obs.ToDesktop(imgX, imgY)
		if abs(gotX-want[0]) > 3 || abs(gotY-want[1]) > 3 {
			t.Errorf("round trip of (%d,%d) came back as (%d,%d)", want[0], want[1], gotX, gotY)
		}
	}
}

func TestToDesktopDoesNotMutateTheObservation(t *testing.T) {
	obs := Observation{
		Width: 1920, Height: 1080, Scale: 0.5, OriginX: 10, OriginY: 20,
		DesktopWidth: 1920, DesktopHeight: 1080,
	}
	before := obs
	_, _ = obs.ToDesktop(500, 500)
	if obs.Width != before.Width || obs.Height != before.Height ||
		obs.Scale != before.Scale || obs.OriginX != before.OriginX ||
		obs.OriginY != before.OriginY ||
		obs.DesktopWidth != before.DesktopWidth || obs.DesktopHeight != before.DesktopHeight {
		t.Errorf("ToDesktop mutated the observation: %+v -> %+v", before, obs)
	}
}

// ----------------------------------------------------------------- clamp ---

func TestClampInt(t *testing.T) {
	cases := []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{11, 0, 10, 10},
		{0, 0, 10, 0},
		{10, 0, 10, 10},
		// An inverted range (a zero-sized display) yields the low bound rather
		// than a nonsense negative.
		{7, 0, -1, 0},
		{-7, 0, -1, 0},
	}
	for _, tc := range cases {
		if got := clampInt(tc.v, tc.lo, tc.hi); got != tc.want {
			t.Errorf("clampInt(%d,%d,%d) = %d, want %d", tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
