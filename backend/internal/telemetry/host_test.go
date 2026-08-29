package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCPULine(t *testing.T) {
	// user nice system idle iowait irq softirq steal
	idle, all, ok := parseCPULine("cpu  100 20 30 700 50 5 5 0")
	if !ok {
		t.Fatal("well-formed line rejected")
	}
	// idle counts idle + iowait, matching what top reports as not-busy.
	if idle != 750 {
		t.Errorf("idle = %d, want 750 (idle 700 + iowait 50)", idle)
	}
	if all != 910 {
		t.Errorf("all = %d, want 910", all)
	}

	if _, _, ok := parseCPULine("cpu0 1 2 3 4 5"); ok {
		t.Error("per-core line should be rejected; only the aggregate is wanted")
	}
	if _, _, ok := parseCPULine("cpu bogus 2 3 4 5"); ok {
		t.Error("non-numeric field should be rejected")
	}
}

// The first sample has no baseline, so it must report not-ok rather than
// inventing 0% -- a dashboard showing a confident 0% is worse than showing
// nothing.
func TestCPUPercentNeedsTwoSamples(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "stat", "cpu  100 0 100 800 0 0 0 0\n")
	c := &HostCollector{procRoot: dir}

	if _, _, ok := c.cpuPercent(); ok {
		t.Fatal("first sample should not report a percentage")
	}

	// Second reading: 100 more busy jiffies, 100 more idle -> 50% busy.
	write(t, dir, "stat", "cpu  200 0 100 900 0 0 0 0\n")
	pct, _, ok := c.cpuPercent()
	if !ok {
		t.Fatal("second sample should report a percentage")
	}
	if pct < 49 || pct > 51 {
		t.Fatalf("cpu = %.1f%%, want ~50%%", pct)
	}
}

func TestParseMeminfoLine(t *testing.T) {
	k, v, ok := parseMeminfoLine("MemTotal:       16316412 kB")
	if !ok || k != "MemTotal" || v != 16316412*1024 {
		t.Fatalf("got %q=%d ok=%v", k, v, ok)
	}
	// A unit-less value must not be multiplied.
	if _, v, _ := parseMeminfoLine("HugePages_Total:       0"); v != 0 {
		t.Fatalf("unit-less value mangled: %d", v)
	}
	if _, _, ok := parseMeminfoLine("garbage"); ok {
		t.Error("line without a colon should be rejected")
	}
}

// Used memory follows MemAvailable, not MemFree: counting reclaimable page
// cache as used makes a healthy machine look nearly full.
func TestMemoryUsesAvailableNotFree(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "meminfo", "MemTotal: 1000 kB\nMemFree: 100 kB\nMemAvailable: 600 kB\nSwapTotal: 200 kB\nSwapFree: 150 kB\n")
	c := &HostCollector{procRoot: dir}

	used, total, swapUsed, swapTotal := c.memory()
	if total != 1000*1024 {
		t.Errorf("total = %d", total)
	}
	if used != 400*1024 {
		t.Errorf("used = %d, want %d (total - available, not total - free)", used, 400*1024)
	}
	if swapTotal != 200*1024 || swapUsed != 50*1024 {
		t.Errorf("swap = %d/%d", swapUsed, swapTotal)
	}
}

func TestParseNvidiaSMI(t *testing.T) {
	g, ok := parseNvidiaSMI("NVIDIA GeForce RTX 3060 Laptop GPU, 4096, 6144, 37, 54\n")
	if !ok {
		t.Fatal("well-formed row rejected")
	}
	if g.Name != "NVIDIA GeForce RTX 3060 Laptop GPU" {
		t.Errorf("name = %q", g.Name)
	}
	if g.MemoryUsed != 4096*1024*1024 || g.MemoryTotal != 6144*1024*1024 {
		t.Errorf("memory MiB not converted to bytes: %d/%d", g.MemoryUsed, g.MemoryTotal)
	}
	if g.Utilisation != 37 || g.TemperatureC != 54 {
		t.Errorf("util/temp = %v/%v", g.Utilisation, g.TemperatureC)
	}

	// Temperature is optional; the rest must still parse without it.
	if g, ok := parseNvidiaSMI("GPU, 1, 2, 3"); !ok || g.TemperatureC != 0 {
		t.Error("row without temperature should still parse")
	}
	if _, ok := parseNvidiaSMI(""); ok {
		t.Error("empty output should be rejected")
	}
	if _, ok := parseNvidiaSMI("no GPU found"); ok {
		t.Error("non-CSV output should be rejected")
	}
}

// A host with no GPU is a normal state. It must be distinguishable from a
// broken collector, so the absence carries a message.
func TestGPUAbsenceIsReportedNotSwallowed(t *testing.T) {
	c := &HostCollector{
		procRoot:  t.TempDir(),
		nvidiaSMI: func(context.Context) (string, error) { return "", errors.New("not found") },
	}
	gpu, msg := c.gpu(context.Background())
	if gpu != nil {
		t.Fatal("expected no GPU")
	}
	if msg == "" {
		t.Fatal("absence must explain itself, or the UI cannot tell 'no GPU' from 'collector broken'")
	}
}

// Every section degrades on its own: an unreadable /proc must not stop the
// snapshot from carrying whatever else it could gather.
func TestSampleSurvivesAnUnreadableProc(t *testing.T) {
	c := &HostCollector{
		procRoot:  filepath.Join(t.TempDir(), "does-not-exist"),
		diskPath:  "/",
		nvidiaSMI: func(context.Context) (string, error) { return "", errors.New("nope") },
	}
	s := c.Sample(context.Background())
	if s.SampledAt.IsZero() {
		t.Fatal("snapshot should still be stamped")
	}
	if s.DiskTotal == 0 {
		t.Fatal("disk should still be readable via statfs")
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Where nvidia-smi cannot be executed -- an Alpine image cannot run the
// glibc-linked binary the NVIDIA runtime injects -- a host-side job writes the
// same CSV to a file and the collector reads that instead.
func TestGPUStatsCanComeFromAFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gpu-stats.csv")
	write(t, dir, "gpu-stats.csv", "NVIDIA GeForce RTX 3060 Laptop GPU, 4096, 6144, 42, 51\n")

	c := &HostCollector{
		procRoot: dir,
		gpuFile:  path,
		// Would succeed if called; the file must take precedence.
		nvidiaSMI: func(context.Context) (string, error) { return "OTHER, 1, 2, 3, 4", nil },
	}

	gpu, msg := c.gpu(context.Background())
	if gpu == nil {
		t.Fatalf("expected stats from the file, got %q", msg)
	}
	if gpu.Name != "NVIDIA GeForce RTX 3060 Laptop GPU" {
		t.Fatalf("file should win over the binary, got %q", gpu.Name)
	}
	if gpu.Utilisation != 42 {
		t.Fatalf("utilisation = %v", gpu.Utilisation)
	}
}

func TestMissingGPUFileExplainsItself(t *testing.T) {
	c := &HostCollector{procRoot: t.TempDir(), gpuFile: "/nope/gpu-stats.csv"}
	gpu, msg := c.gpu(context.Background())
	if gpu != nil {
		t.Fatal("expected no stats")
	}
	if !strings.Contains(msg, "gpu-stats.csv") {
		t.Fatalf("message should name the file it could not read: %q", msg)
	}
}
