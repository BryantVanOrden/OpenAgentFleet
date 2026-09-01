// Package fleet provisions and supervises sandboxed operating systems.
//
// The Docker driver talks to the engine over its REST API directly rather than
// through the official SDK: the surface we need is small (create / start / stop
// / pause / inspect / stats / exec / network) and this keeps the orchestrator
// dependency-light and easy to audit.
package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const dockerAPIVersion = "v1.43"

type DockerClient struct {
	http *http.Client
	base string // e.g. http://docker or http://127.0.0.1:2375
}

// NewDockerClient accepts unix:///var/run/docker.sock, tcp://host:port or
// http(s)://host:port. Windows named pipes are not supported directly — set
// DOCKER_HOST to the engine's TCP endpoint, or run the orchestrator in Docker
// with the socket bind-mounted (which is what docker-compose.yml does).
func NewDockerClient(host string) (*DockerClient, error) {
	switch {
	case strings.HasPrefix(host, "unix://"):
		path := strings.TrimPrefix(host, "unix://")
		return &DockerClient{
			base: "http://docker",
			http: &http.Client{
				Timeout: 5 * time.Minute,
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
						var d net.Dialer
						return d.DialContext(ctx, "unix", path)
					},
				},
			},
		}, nil

	case strings.HasPrefix(host, "tcp://"):
		return &DockerClient{
			base: "http://" + strings.TrimPrefix(host, "tcp://"),
			http: &http.Client{Timeout: 5 * time.Minute},
		}, nil

	case strings.HasPrefix(host, "http://"), strings.HasPrefix(host, "https://"):
		return &DockerClient{base: strings.TrimSuffix(host, "/"), http: &http.Client{Timeout: 5 * time.Minute}}, nil

	case strings.HasPrefix(host, "npipe://"):
		return nil, fmt.Errorf("npipe DOCKER_HOST is unsupported on %s: expose the engine on tcp:// "+
			"or run the orchestrator inside Docker with /var/run/docker.sock mounted", runtime.GOOS)

	default:
		return nil, fmt.Errorf("unrecognised DOCKER_HOST %q", host)
	}
}

func (c *DockerClient) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+"/"+dockerAPIVersion+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &DockerError{Status: resp.StatusCode, Body: strings.TrimSpace(string(msg)), Path: path}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type DockerError struct {
	Status int
	Path   string
	Body   string
}

func (e *DockerError) Error() string {
	return fmt.Sprintf("docker %s: %d: %s", e.Path, e.Status, e.Body)
}

func (e *DockerError) NotFound() bool { return e.Status == http.StatusNotFound }

// ------------------------------------------------------------- API payloads ---

type containerCreate struct {
	Image            string              `json:"Image"`
	Hostname         string              `json:"Hostname,omitempty"`
	Env              []string            `json:"Env,omitempty"`
	Labels           map[string]string   `json:"Labels,omitempty"`
	ExposedPorts     map[string]struct{} `json:"ExposedPorts,omitempty"`
	HostConfig       hostConfig          `json:"HostConfig"`
	NetworkingConfig *networkingConfig   `json:"NetworkingConfig,omitempty"`
}

type hostConfig struct {
	Memory         int64                 `json:"Memory,omitempty"`
	MemorySwap     int64                 `json:"MemorySwap,omitempty"`
	NanoCPUs       int64                 `json:"NanoCpus,omitempty"`
	PidsLimit      *int64                `json:"PidsLimit,omitempty"`
	ShmSize        int64                 `json:"ShmSize,omitempty"`
	CapAdd         []string              `json:"CapAdd,omitempty"`
	SecurityOpt    []string              `json:"SecurityOpt,omitempty"`
	Privileged     bool                  `json:"Privileged,omitempty"`
	NetworkMode    string                `json:"NetworkMode,omitempty"`
	PortBindings   map[string][]portBind `json:"PortBindings,omitempty"`
	RestartPolicy  restartPolicy         `json:"RestartPolicy"`
	Mounts         []mount               `json:"Mounts,omitempty"`
	StorageOpt     map[string]string     `json:"StorageOpt,omitempty"`
	DeviceRequests []deviceRequest       `json:"DeviceRequests,omitempty"`
	// Devices maps host device nodes into the container — /dev/kvm for the
	// qemu driver. Distinct from DeviceRequests, which is the GPU plugin path.
	Devices []deviceMapping `json:"Devices,omitempty"`
	Tmpfs          map[string]string     `json:"Tmpfs,omitempty"`
	Ulimits        []ulimit              `json:"Ulimits,omitempty"`
}

type deviceMapping struct {
	PathOnHost        string `json:"PathOnHost"`
	PathInContainer   string `json:"PathInContainer"`
	CgroupPermissions string `json:"CgroupPermissions"`
}

type portBind struct {
	HostIP   string `json:"HostIp,omitempty"`
	HostPort string `json:"HostPort,omitempty"`
}

type restartPolicy struct {
	Name string `json:"Name"`
}

type mount struct {
	Type     string `json:"Type"`
	Source   string `json:"Source,omitempty"`
	Target   string `json:"Target"`
	ReadOnly bool   `json:"ReadOnly,omitempty"`
}

type deviceRequest struct {
	Driver       string     `json:"Driver,omitempty"`
	Count        int        `json:"Count,omitempty"`
	Capabilities [][]string `json:"Capabilities,omitempty"`
}

type ulimit struct {
	Name string `json:"Name"`
	Soft int64  `json:"Soft"`
	Hard int64  `json:"Hard"`
}

type networkingConfig struct {
	EndpointsConfig map[string]endpointSettings `json:"EndpointsConfig"`
}

type endpointSettings struct {
	Aliases []string `json:"Aliases,omitempty"`
}

type createResponse struct {
	ID       string   `json:"Id"`
	Warnings []string `json:"Warnings"`
}

// ContainerInspect is the trimmed subset of the inspect payload we use.
type ContainerInspect struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Paused     bool   `json:"Paused"`
		ExitCode   int    `json:"ExitCode"`
		OOMKilled  bool   `json:"OOMKilled"`
		Error      string `json:"Error"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
	} `json:"State"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
		Ports map[string][]portBind `json:"Ports"`
	} `json:"NetworkSettings"`
}

// IPOn returns the container address on a named network.
func (ci *ContainerInspect) IPOn(network string) string {
	if n, ok := ci.NetworkSettings.Networks[network]; ok {
		return n.IPAddress
	}
	for _, n := range ci.NetworkSettings.Networks {
		if n.IPAddress != "" {
			return n.IPAddress
		}
	}
	return ""
}

// HostPort resolves the published host port for a container port ("6901/tcp").
func (ci *ContainerInspect) HostPort(containerPort string) string {
	for _, b := range ci.NetworkSettings.Ports[containerPort] {
		if b.HostPort != "" {
			return b.HostPort
		}
	}
	return ""
}

// StatsSample is the one-shot /stats payload.
type StatsSample struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage int64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage int64 `json:"system_cpu_usage"`
		OnlineCPUs  int   `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage int64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage int64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage int64 `json:"usage"`
		Limit int64 `json:"limit"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes int64 `json:"rx_bytes"`
		TxBytes int64 `json:"tx_bytes"`
	} `json:"networks"`
}

// ------------------------------------------------------------- operations ---

func (c *DockerClient) Ping(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/_ping", nil, nil)
}

// HostCapacity is what the engine says the host actually has.
type HostCapacity struct {
	// NCPU is the number of CPUs Docker will admit. Asking for more is a hard
	// 400 from the engine, not a best-effort scheduling hint.
	NCPU int `json:"NCPU"`
	// MemTotal is host RAM in bytes.
	MemTotal int64 `json:"MemTotal"`
	// Runtimes is the engine's configured OCI runtimes. An "nvidia" entry means
	// the NVIDIA container toolkit is installed; without it, asking for a GPU
	// fails the container at start with a prestart-hook error rather than
	// falling back to CPU.
	Runtimes map[string]struct {
		Path string `json:"path"`
	} `json:"Runtimes"`
}

// HasNVIDIARuntime reports whether a GPU request can possibly succeed.
func (h HostCapacity) HasNVIDIARuntime() bool {
	_, ok := h.Runtimes["nvidia"]
	return ok
}

// Info reports the host's capacity, for clamping a tier to what can actually run.
func (c *DockerClient) Info(ctx context.Context) (HostCapacity, error) {
	var out HostCapacity
	if err := c.do(ctx, http.MethodGet, "/info", nil, &out); err != nil {
		return HostCapacity{}, err
	}
	return out, nil
}

func (c *DockerClient) CreateContainer(ctx context.Context, name string, spec containerCreate) (string, error) {
	var out createResponse
	err := c.do(ctx, http.MethodPost, "/containers/create?name="+name, spec, &out)
	if err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *DockerClient) StartContainer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/containers/"+id+"/start", nil, nil)
}

func (c *DockerClient) StopContainer(ctx context.Context, id string, timeoutSec int) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/stop?t=%d", id, timeoutSec), nil, nil)
}

func (c *DockerClient) PauseContainer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/containers/"+id+"/pause", nil, nil)
}

func (c *DockerClient) UnpauseContainer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/containers/"+id+"/unpause", nil, nil)
}

func (c *DockerClient) RemoveContainer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/containers/"+id+"?force=true&v=true", nil, nil)
}

func (c *DockerClient) InspectContainer(ctx context.Context, id string) (*ContainerInspect, error) {
	var out ContainerInspect
	if err := c.do(ctx, http.MethodGet, "/containers/"+id+"/json", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *DockerClient) Stats(ctx context.Context, id string) (*StatsSample, error) {
	var out StatsSample
	if err := c.do(ctx, http.MethodGet, "/containers/"+id+"/stats?stream=false&one-shot=false", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EnsureNetwork creates a bridge network if it does not already exist.
// `internal` cuts the sandbox off from the outside world entirely.
func (c *DockerClient) EnsureNetwork(ctx context.Context, name string, internal bool) error {
	err := c.do(ctx, http.MethodGet, "/networks/"+name, nil, nil)
	if err == nil {
		return nil
	}
	var de *DockerError
	if ok := asDockerError(err, &de); !ok || !de.NotFound() {
		return err
	}
	return c.do(ctx, http.MethodPost, "/networks/create", map[string]any{
		"Name":     name,
		"Driver":   "bridge",
		"Internal": internal,
		"Labels":   map[string]string{"managed-by": "agentfleet"},
	}, nil)
}

// ImageExists reports whether the engine already has the image locally.
func (c *DockerClient) ImageExists(ctx context.Context, ref string) bool {
	return c.do(ctx, http.MethodGet, "/images/"+ref+"/json", nil, nil) == nil
}

// PullImage streams a pull to completion, discarding progress output.
func (c *DockerClient) PullImage(ctx context.Context, ref string) error {
	name, tag := ref, "latest"
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		name, tag = ref[:i], ref[i+1:]
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/%s/images/create?fromImage=%s&tag=%s", c.base, dockerAPIVersion, name, tag), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("pull %s: %d: %s", ref, resp.StatusCode, msg)
	}
	_, err = io.Copy(io.Discard, resp.Body)
	return err
}

// Exec runs a command inside a container and returns the multiplexed output.
func (c *DockerClient) Exec(ctx context.Context, id string, cmd []string) (string, error) {
	return c.ExecAs(ctx, id, "", cmd)
}

// ExecAs runs a command as a specific user. An empty user keeps the image's
// default, which is the unprivileged agent; "0" is how the platform changes
// something the agent itself must not be able to change.
func (c *DockerClient) ExecAs(ctx context.Context, id, user string, cmd []string) (string, error) {
	payload := map[string]any{
		"AttachStdout": true, "AttachStderr": true, "Cmd": cmd, "Tty": false,
	}
	if user != "" {
		payload["User"] = user
	}
	var created createResponse
	err := c.do(ctx, http.MethodPost, "/containers/"+id+"/exec", payload, &created)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]any{"Detach": false, "Tty": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/%s/exec/%s/start", c.base, dockerAPIVersion, created.ID), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		// Without this the engine's own error body was demultiplexed and
		// returned as though it were the command's stdout, with a nil error. A
		// caller probing a paused container was told the probe had run and
		// printed something.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", &DockerError{
			Status: resp.StatusCode,
			Body:   strings.TrimSpace(string(msg)),
			Path:   "/exec/" + created.ID + "/start",
		}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return demultiplex(raw), nil
}

// demultiplex strips Docker's 8-byte stream framing from non-TTY exec output.
func demultiplex(raw []byte) string {
	var sb strings.Builder
	for len(raw) >= 8 {
		size := int(raw[4])<<24 | int(raw[5])<<16 | int(raw[6])<<8 | int(raw[7])
		if size < 0 || 8+size > len(raw) {
			sb.Write(raw[8:])
			return sb.String()
		}
		sb.Write(raw[8 : 8+size])
		raw = raw[8+size:]
	}
	return sb.String()
}

func asDockerError(err error, target **DockerError) bool {
	if de, ok := err.(*DockerError); ok {
		*target = de
		return true
	}
	return false
}
