package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Container is an entry of GET /containers/json.
type Container struct {
	ID              string            `json:"Id"`
	Names           []string          `json:"Names"`
	Image           string            `json:"Image"`
	ImageID         string            `json:"ImageID"`
	Command         string            `json:"Command"`
	Created         int64             `json:"Created"`
	State           string            `json:"State"`
	Status          string            `json:"Status"`
	Ports           []Port            `json:"Ports"`
	Labels          map[string]string `json:"Labels"`
	Mounts          []MountPoint      `json:"Mounts"`
	NetworkSettings struct {
		Networks map[string]EndpointSettings `json:"Networks"`
	} `json:"NetworkSettings"`
}

// Name returns the primary container name without the leading slash.
func (c Container) Name() string {
	if len(c.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

type Port struct {
	IP          string `json:"IP,omitempty"`
	PrivatePort uint16 `json:"PrivatePort"`
	PublicPort  uint16 `json:"PublicPort,omitempty"`
	Type        string `json:"Type"`
}

type MountPoint struct {
	Type        string `json:"Type"`
	Name        string `json:"Name,omitempty"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	Mode        string `json:"Mode"`
	RW          bool   `json:"RW"`
}

// ContainerJSON is the response of GET /containers/{id}/json.
type ContainerJSON struct {
	ID              string           `json:"Id"`
	Name            string           `json:"Name"`
	Created         time.Time        `json:"Created"`
	Path            string           `json:"Path"`
	Args            []string         `json:"Args"`
	Image           string           `json:"Image"`
	RestartCount    int              `json:"RestartCount"`
	State           *ContainerState  `json:"State"`
	Config          *ContainerConfig `json:"Config"`
	HostConfig      *HostConfig      `json:"HostConfig"`
	NetworkSettings *NetworkSettings `json:"NetworkSettings"`
	Mounts          []MountPoint     `json:"Mounts"`
}

type ContainerState struct {
	Status     string    `json:"Status"`
	Running    bool      `json:"Running"`
	Paused     bool      `json:"Paused"`
	Restarting bool      `json:"Restarting"`
	OOMKilled  bool      `json:"OOMKilled"`
	Dead       bool      `json:"Dead"`
	Pid        int       `json:"Pid"`
	ExitCode   int       `json:"ExitCode"`
	Error      string    `json:"Error"`
	StartedAt  time.Time `json:"StartedAt"`
	FinishedAt time.Time `json:"FinishedAt"`
	Health     *struct {
		Status        string `json:"Status"`
		FailingStreak int    `json:"FailingStreak"`
	} `json:"Health,omitempty"`
}

// ContainerConfig is the portable part of a container's configuration; it is
// shared by inspect, image config and create.
type ContainerConfig struct {
	Hostname     string              `json:"Hostname,omitempty"`
	User         string              `json:"User,omitempty"`
	Env          []string            `json:"Env,omitempty"`
	Cmd          []string            `json:"Cmd,omitempty"`
	Entrypoint   []string            `json:"Entrypoint,omitempty"`
	Image        string              `json:"Image,omitempty"`
	WorkingDir   string              `json:"WorkingDir,omitempty"`
	Labels       map[string]string   `json:"Labels,omitempty"`
	ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
	Volumes      map[string]struct{} `json:"Volumes,omitempty"`
	Tty          bool                `json:"Tty,omitempty"`
	OpenStdin    bool                `json:"OpenStdin,omitempty"`
}

type HostConfig struct {
	Binds         []string                 `json:"Binds,omitempty"`
	PortBindings  map[string][]PortBinding `json:"PortBindings,omitempty"`
	RestartPolicy RestartPolicy            `json:"RestartPolicy,omitzero"`
	NetworkMode   string                   `json:"NetworkMode,omitempty"`
	Privileged    bool                     `json:"Privileged,omitempty"`
	AutoRemove    bool                     `json:"AutoRemove,omitempty"`
	Memory        int64                    `json:"Memory,omitempty"`
	NanoCPUs      int64                    `json:"NanoCpus,omitempty"`
}

type PortBinding struct {
	HostIP   string `json:"HostIp,omitempty"`
	HostPort string `json:"HostPort,omitempty"`
}

type RestartPolicy struct {
	Name              string `json:"Name,omitempty"` // "", no, always, unless-stopped, on-failure
	MaximumRetryCount int    `json:"MaximumRetryCount,omitempty"`
}

type NetworkSettings struct {
	Ports    map[string][]PortBinding    `json:"Ports"`
	Networks map[string]EndpointSettings `json:"Networks"`
}

type EndpointSettings struct {
	NetworkID   string   `json:"NetworkID,omitempty"`
	EndpointID  string   `json:"EndpointID,omitempty"`
	Gateway     string   `json:"Gateway,omitempty"`
	IPAddress   string   `json:"IPAddress,omitempty"`
	IPv6Address string   `json:"GlobalIPv6Address,omitempty"`
	MacAddress  string   `json:"MacAddress,omitempty"`
	Aliases     []string `json:"Aliases,omitempty"`
}

// ContainerCreate is the body of POST /containers/create.
type ContainerCreate struct {
	ContainerConfig
	HostConfig       *HostConfig       `json:"HostConfig,omitempty"`
	NetworkingConfig *NetworkingConfig `json:"NetworkingConfig,omitempty"`
}

type NetworkingConfig struct {
	EndpointsConfig map[string]EndpointSettings `json:"EndpointsConfig,omitempty"`
}

type ListContainersOptions struct {
	All     bool
	Filters Filters
}

func (c *Client) ListContainers(ctx context.Context, opts ListContainersOptions) ([]Container, error) {
	q := url.Values{}
	if opts.All {
		q.Set("all", "1")
	}
	opts.Filters.encode(q)
	var out []Container
	err := c.call(ctx, request{method: http.MethodGet, path: "/containers/json", query: q}, &out)
	return out, err
}

func (c *Client) InspectContainer(ctx context.Context, id string) (*ContainerJSON, error) {
	var out ContainerJSON
	if err := c.call(ctx, request{method: http.MethodGet, path: "/containers/" + url.PathEscape(id) + "/json"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateContainer creates a container and returns its ID.
func (c *Client) CreateContainer(ctx context.Context, name string, spec ContainerCreate) (string, error) {
	q := url.Values{}
	if name != "" {
		q.Set("name", name)
	}
	var out struct {
		ID string `json:"Id"`
	}
	err := c.call(ctx, request{method: http.MethodPost, path: "/containers/create", query: q, json: spec}, &out)
	return out.ID, err
}

func (c *Client) containerAction(ctx context.Context, id, action string, q url.Values) error {
	return c.call(ctx, request{method: http.MethodPost, path: "/containers/" + url.PathEscape(id) + "/" + action, query: q}, nil)
}

// StartContainer starts a container; starting a running container succeeds.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.containerAction(ctx, id, "start", nil)
}

// StopContainer stops a container, killing it after timeout. A nil timeout
// uses the container's configured stop timeout.
func (c *Client) StopContainer(ctx context.Context, id string, timeout *time.Duration) error {
	return c.containerAction(ctx, id, "stop", timeoutQuery(timeout))
}

func (c *Client) RestartContainer(ctx context.Context, id string, timeout *time.Duration) error {
	return c.containerAction(ctx, id, "restart", timeoutQuery(timeout))
}

func (c *Client) KillContainer(ctx context.Context, id, signal string) error {
	q := url.Values{}
	if signal != "" {
		q.Set("signal", signal)
	}
	return c.containerAction(ctx, id, "kill", q)
}

func (c *Client) PauseContainer(ctx context.Context, id string) error {
	return c.containerAction(ctx, id, "pause", nil)
}

func (c *Client) UnpauseContainer(ctx context.Context, id string) error {
	return c.containerAction(ctx, id, "unpause", nil)
}

func (c *Client) RenameContainer(ctx context.Context, id, name string) error {
	return c.containerAction(ctx, id, "rename", url.Values{"name": {name}})
}

func timeoutQuery(timeout *time.Duration) url.Values {
	if timeout == nil {
		return nil
	}
	return url.Values{"t": {strconv.Itoa(int(timeout.Seconds()))}}
}

type RemoveContainerOptions struct {
	Force         bool // kill a running container first
	RemoveVolumes bool // remove anonymous volumes
}

func (c *Client) RemoveContainer(ctx context.Context, id string, opts RemoveContainerOptions) error {
	q := url.Values{"force": {boolString(opts.Force)}, "v": {boolString(opts.RemoveVolumes)}}
	return c.call(ctx, request{method: http.MethodDelete, path: "/containers/" + url.PathEscape(id), query: q}, nil)
}

// PruneReport summarises a prune operation.
type PruneReport struct {
	Deleted        []string `json:"deleted"`
	SpaceReclaimed uint64   `json:"space_reclaimed"`
}

func (c *Client) PruneContainers(ctx context.Context) (PruneReport, error) {
	var out struct {
		ContainersDeleted []string
		SpaceReclaimed    uint64
	}
	err := c.call(ctx, request{method: http.MethodPost, path: "/containers/prune"}, &out)
	return PruneReport{Deleted: out.ContainersDeleted, SpaceReclaimed: out.SpaceReclaimed}, err
}

type LogsOptions struct {
	Follow     bool
	Tail       string // "all" or a number of lines
	Since      time.Time
	Timestamps bool
}

// LogLine is one line of container output.
type LogLine struct {
	Stream Stream `json:"stream"`
	Text   string `json:"text"`
}

// Logs streams a container's logs line by line until the stream ends, ctx
// is cancelled or fn returns an error.
func (c *Client) Logs(ctx context.Context, id string, opts LogsOptions, fn func(LogLine) error) error {
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "follow": {boolString(opts.Follow)}, "timestamps": {boolString(opts.Timestamps)}}
	if opts.Tail != "" {
		q.Set("tail", opts.Tail)
	}
	if !opts.Since.IsZero() {
		q.Set("since", strconv.FormatInt(opts.Since.Unix(), 10))
	}
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/containers/" + url.PathEscape(id) + "/logs", query: q})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	lines := newLineSplitter(func(s Stream, text string) error { return fn(LogLine{Stream: s, Text: text}) })
	err = readFrames(resp.Body, resp.Header.Get("Content-Type") == multiplexed, lines.write)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	return lines.flush()
}

// StatsJSON is one sample of GET /containers/{id}/stats.
type StatsJSON struct {
	Read     time.Time `json:"read"`
	CPUStats cpuStats  `json:"cpu_stats"`
	PreCPU   cpuStats  `json:"precpu_stats"`
	Memory   struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkIO struct {
		Entries []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	PIDs struct {
		Current uint64 `json:"current"`
	} `json:"pids_stats"`
}

type cpuStats struct {
	Usage struct {
		Total  uint64   `json:"total_usage"`
		PerCPU []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	System uint64 `json:"system_cpu_usage"`
	Online uint32 `json:"online_cpus"`
}

// Stats is a derived, presentation-ready resource usage sample.
type Stats struct {
	Time          time.Time `json:"time"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryUsage   uint64    `json:"memory_usage"`
	MemoryLimit   uint64    `json:"memory_limit"`
	MemoryPercent float64   `json:"memory_percent"`
	NetworkRx     uint64    `json:"network_rx"`
	NetworkTx     uint64    `json:"network_tx"`
	BlockRead     uint64    `json:"block_read"`
	BlockWrite    uint64    `json:"block_write"`
	PIDs          uint64    `json:"pids"`
}

// Summary computes usage the same way `docker stats` does.
func (s *StatsJSON) Summary() Stats {
	out := Stats{Time: s.Read, MemoryLimit: s.Memory.Limit, PIDs: s.PIDs.Current}
	cpuDelta := float64(s.CPUStats.Usage.Total) - float64(s.PreCPU.Usage.Total)
	systemDelta := float64(s.CPUStats.System) - float64(s.PreCPU.System)
	cpus := float64(s.CPUStats.Online)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.Usage.PerCPU))
	}
	if cpuDelta > 0 && systemDelta > 0 && cpus > 0 {
		out.CPUPercent = cpuDelta / systemDelta * cpus * 100
	}
	// Page cache is reclaimable, so docker excludes it from usage: cgroup v2
	// reports it as inactive_file, cgroup v1 as total_inactive_file.
	out.MemoryUsage = s.Memory.Usage
	for _, key := range []string{"inactive_file", "total_inactive_file"} {
		if v, ok := s.Memory.Stats[key]; ok && v < out.MemoryUsage {
			out.MemoryUsage -= v
			break
		}
	}
	if s.Memory.Limit > 0 {
		out.MemoryPercent = float64(out.MemoryUsage) / float64(s.Memory.Limit) * 100
	}
	for _, n := range s.Networks {
		out.NetworkRx += n.RxBytes
		out.NetworkTx += n.TxBytes
	}
	for _, e := range s.BlkIO.Entries {
		switch strings.ToLower(e.Op) {
		case "read":
			out.BlockRead += e.Value
		case "write":
			out.BlockWrite += e.Value
		}
	}
	return out
}

// Stats streams resource usage samples (about one per second) until ctx is
// cancelled or fn returns an error.
func (c *Client) Stats(ctx context.Context, id string, fn func(Stats) error) error {
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/containers/" + url.PathEscape(id) + "/stats", query: url.Values{"stream": {"1"}}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	for {
		var sample StatsJSON
		if err := decoder.Decode(&sample); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := fn(sample.Summary()); err != nil {
			return err
		}
	}
}

// ExecConfig is the body of POST /containers/{id}/exec.
type ExecConfig struct {
	Cmd          []string `json:"Cmd"`
	Env          []string `json:"Env,omitempty"`
	User         string   `json:"User,omitempty"`
	WorkingDir   string   `json:"WorkingDir,omitempty"`
	Tty          bool     `json:"Tty"`
	AttachStdin  bool     `json:"AttachStdin"`
	AttachStdout bool     `json:"AttachStdout"`
	AttachStderr bool     `json:"AttachStderr"`
	ConsoleSize  *[2]uint `json:"ConsoleSize,omitempty"` // height, width
}

func (c *Client) CreateExec(ctx context.Context, containerID string, cfg ExecConfig) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	err := c.call(ctx, request{method: http.MethodPost, path: "/containers/" + url.PathEscape(containerID) + "/exec", json: cfg}, &out)
	return out.ID, err
}

// AttachExec starts an exec instance and returns the raw stream. With a TTY
// the stream is unframed; without one it is multiplexed.
func (c *Client) AttachExec(ctx context.Context, execID string, tty bool, size *[2]uint) (*HijackedConn, error) {
	body := map[string]any{"Detach": false, "Tty": tty}
	if size != nil {
		body["ConsoleSize"] = size
	}
	return c.hijack(ctx, "/exec/"+url.PathEscape(execID)+"/start", body)
}

func (c *Client) ResizeExec(ctx context.Context, execID string, height, width uint) error {
	q := url.Values{"h": {strconv.FormatUint(uint64(height), 10)}, "w": {strconv.FormatUint(uint64(width), 10)}}
	return c.call(ctx, request{method: http.MethodPost, path: "/exec/" + url.PathEscape(execID) + "/resize", query: q}, nil)
}

type ExecInspect struct {
	ID       string `json:"ID"`
	Running  bool   `json:"Running"`
	ExitCode int    `json:"ExitCode"`
	Pid      int    `json:"Pid"`
}

func (c *Client) InspectExec(ctx context.Context, execID string) (*ExecInspect, error) {
	var out ExecInspect
	if err := c.call(ctx, request{method: http.MethodGet, path: "/exec/" + url.PathEscape(execID) + "/json"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
