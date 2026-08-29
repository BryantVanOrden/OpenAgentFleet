package telemetry

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// HostStats is a snapshot of the machine running the orchestrator -- not of a
// sandbox. Per-instance numbers already come from the Docker stats API; this
// answers "is the box that hosts the fleet in trouble", which nothing else did.
type HostStats struct {
	CPUPercent  float64 `json:"cpu_percent"`
	CPUCores    int     `json:"cpu_cores"`
	Load1       float64 `json:"load1"`
	MemoryUsed  uint64  `json:"memory_used_bytes"`
	MemoryTotal uint64  `json:"memory_total_bytes"`
	SwapUsed    uint64  `json:"swap_used_bytes"`
	SwapTotal   uint64  `json:"swap_total_bytes"`
	DiskUsed    uint64  `json:"disk_used_bytes"`
	DiskTotal   uint64  `json:"disk_total_bytes"`
	UptimeSec   float64 `json:"uptime_sec"`

	// GPU is absent on hosts without one, or where nvidia-smi is not reachable
	// from inside the container. Absent is a legitimate state, not an error.
	GPU        *GPUStats `json:"gpu,omitempty"`
	GPUMessage string    `json:"gpu_message,omitempty"`

	SampledAt time.Time `json:"sampled_at"`
}

type GPUStats struct {
	Name         string  `json:"name"`
	MemoryUsed   uint64  `json:"memory_used_bytes"`
	MemoryTotal  uint64  `json:"memory_total_bytes"`
	Utilisation  float64 `json:"utilisation_percent"`
	TemperatureC float64 `json:"temperature_c,omitempty"`
}

// HostCollector samples /proc. CPU percent needs two readings to mean anything,
// so the previous jiffy counters are kept between calls.
type HostCollector struct {
	mu       sync.Mutex
	prevIdle uint64
	prevAll  uint64

	// procRoot and diskPath are injectable so the parsing can be tested against
	// fixtures rather than whatever the build machine happens to look like.
	procRoot  string
	diskPath  string
	nvidiaSMI func(ctx context.Context) (string, error)
	// gpuFile is a fallback for deployments where nvidia-smi cannot run in
	// process -- an Alpine container, for instance, cannot execute the
	// glibc-linked binary the NVIDIA runtime injects. A host-side job writes
	// the same CSV there and this reads it.
	gpuFile string
}

func NewHostCollector() *HostCollector {
	// Inside a container /proc/stat and /proc/meminfo are the host's -- those
	// files are not namespaced -- so CPU and memory are already host figures.
	// The filesystem is not: "/" is the container's own layer. HOST_DISK_PATH
	// lets a deployment bind-mount the real root somewhere and point at it,
	// rather than this silently reporting the image's disk as the host's.
	return &HostCollector{
		procRoot:  envOr("HOST_PROC_PATH", "/proc"),
		diskPath:  envOr("HOST_DISK_PATH", "/"),
		nvidiaSMI: runNvidiaSMI,
		gpuFile:   os.Getenv("HOST_GPU_STATS_FILE"),
	}
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// Sample reads a fresh snapshot. Every section degrades independently: a
// container with no /proc/meminfo still reports CPU, and a host with no GPU
// still reports memory.
func (c *HostCollector) Sample(ctx context.Context) HostStats {
	s := HostStats{SampledAt: time.Now().UTC(), CPUCores: numCPU()}

	if used, total, ok := c.cpuPercent(); ok {
		s.CPUPercent = used
		_ = total
	}
	s.Load1 = c.load1()
	s.MemoryUsed, s.MemoryTotal, s.SwapUsed, s.SwapTotal = c.memory()
	s.DiskUsed, s.DiskTotal = c.disk()
	s.UptimeSec = c.uptime()

	if gpu, msg := c.gpu(ctx); gpu != nil {
		s.GPU = gpu
	} else {
		s.GPUMessage = msg
	}
	return s
}

func numCPU() int {
	n := 0
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "processor") {
			n++
		}
	}
	return n
}

// cpuPercent returns busy time since the previous call. The first call has no
// baseline and reports not-ok rather than a fabricated 0%.
func (c *HostCollector) cpuPercent() (float64, float64, bool) {
	line, err := firstLinePrefixed(c.procRoot+"/stat", "cpu ")
	if err != nil {
		return 0, 0, false
	}
	idle, all, ok := parseCPULine(line)
	if !ok {
		return 0, 0, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	prevIdle, prevAll := c.prevIdle, c.prevAll
	c.prevIdle, c.prevAll = idle, all
	if prevAll == 0 || all <= prevAll {
		return 0, 0, false
	}

	dIdle := float64(idle - prevIdle)
	dAll := float64(all - prevAll)
	return (1 - dIdle/dAll) * 100, dAll, true
}

// parseCPULine turns "cpu  u n s i io irq soft steal ..." into idle and total
// jiffies. Idle counts both idle and iowait, matching what top reports.
func parseCPULine(line string) (idle, all uint64, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		all += v
		if i == 3 || i == 4 { // idle, iowait
			idle += v
		}
	}
	return idle, all, true
}

func (c *HostCollector) load1() float64 {
	b, err := os.ReadFile(c.procRoot + "/loadavg")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func (c *HostCollector) memory() (used, total, swapUsed, swapTotal uint64) {
	vals := map[string]uint64{}
	f, err := os.Open(c.procRoot + "/meminfo")
	if err != nil {
		return 0, 0, 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := parseMeminfoLine(sc.Text())
		if ok {
			vals[k] = v
		}
	}
	total = vals["MemTotal"]
	// MemAvailable is the honest figure: free alone counts reclaimable page
	// cache as used and makes every machine look nearly full.
	if avail, ok := vals["MemAvailable"]; ok && total >= avail {
		used = total - avail
	}
	swapTotal = vals["SwapTotal"]
	if free, ok := vals["SwapFree"]; ok && swapTotal >= free {
		swapUsed = swapTotal - free
	}
	return used, total, swapUsed, swapTotal
}

// parseMeminfoLine handles "MemTotal:       16316412 kB" -> ("MemTotal", bytes).
func parseMeminfoLine(line string) (string, uint64, bool) {
	name, rest, found := strings.Cut(line, ":")
	if !found {
		return "", 0, false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", 0, false
	}
	v, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return "", 0, false
	}
	if len(fields) > 1 && strings.EqualFold(fields[1], "kB") {
		v *= 1024
	}
	return name, v, true
}

func (c *HostCollector) disk() (used, total uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(c.diskPath, &st); err != nil {
		return 0, 0
	}
	bs := uint64(st.Bsize)
	total = st.Blocks * bs
	// Blocks reserved for root are neither free nor usable; counting them as
	// used matches df's "Use%" rather than overstating headroom.
	used = (st.Blocks - st.Bfree) * bs
	return used, total
}

func (c *HostCollector) uptime() float64 {
	b, err := os.ReadFile(c.procRoot + "/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func runNvidiaSMI(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,memory.used,memory.total,utilization.gpu,temperature.gpu",
		"--format=csv,noheader,nounits").Output()
	return string(out), err
}

// gpu shells out to nvidia-smi. The orchestrator normally runs in a container
// without the NVIDIA runtime, so "no GPU visible" is the common case and is
// reported as a message rather than swallowed -- otherwise the UI cannot tell
// "no GPU" from "collector broken".
func (c *HostCollector) gpu(ctx context.Context) (*GPUStats, string) {
	// The file wins when configured: where it exists, running the binary is
	// known not to work, and trying first would just cost a failed exec.
	if c.gpuFile != "" {
		b, err := os.ReadFile(c.gpuFile)
		if err != nil {
			return nil, "no GPU reading: " + c.gpuFile + " is not readable"
		}
		if g, ok := parseNvidiaSMI(string(b)); ok {
			return g, ""
		}
		return nil, "no GPU reading: could not parse " + c.gpuFile
	}

	out, err := c.nvidiaSMI(ctx)
	if err != nil {
		return nil, "no GPU reading: nvidia-smi unavailable to the orchestrator"
	}
	g, ok := parseNvidiaSMI(out)
	if !ok {
		return nil, "no GPU reading: could not parse nvidia-smi output"
	}
	return g, ""
}

// parseNvidiaSMI reads one CSV row of "name, usedMiB, totalMiB, util%, tempC".
func parseNvidiaSMI(out string) (*GPUStats, bool) {
	line := strings.TrimSpace(firstNonBlank(out))
	if line == "" {
		return nil, false
	}
	parts := strings.Split(line, ",")
	if len(parts) < 4 {
		return nil, false
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	used, err1 := strconv.ParseUint(parts[1], 10, 64)
	total, err2 := strconv.ParseUint(parts[2], 10, 64)
	util, err3 := strconv.ParseFloat(parts[3], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, false
	}
	g := &GPUStats{
		Name:        parts[0],
		MemoryUsed:  used * 1024 * 1024,
		MemoryTotal: total * 1024 * 1024,
		Utilisation: util,
	}
	if len(parts) > 4 {
		if temp, err := strconv.ParseFloat(parts[4], 64); err == nil {
			g.TemperatureC = temp
		}
	}
	return g, true
}

func firstNonBlank(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

func firstLinePrefixed(path, prefix string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), prefix) {
			return sc.Text(), nil
		}
	}
	return "", os.ErrNotExist
}
