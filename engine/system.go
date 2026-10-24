package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

type Version struct {
	Version       string `json:"Version"`
	APIVersion    string `json:"ApiVersion"`
	MinAPIVersion string `json:"MinAPIVersion"`
	GitCommit     string `json:"GitCommit"`
	GoVersion     string `json:"GoVersion"`
	Os            string `json:"Os"`
	Arch          string `json:"Arch"`
	KernelVersion string `json:"KernelVersion"`
}

type Info struct {
	ID                string `json:"ID"`
	Name              string `json:"Name"`
	ServerVersion     string `json:"ServerVersion"`
	OperatingSystem   string `json:"OperatingSystem"`
	OSType            string `json:"OSType"`
	Architecture      string `json:"Architecture"`
	KernelVersion     string `json:"KernelVersion"`
	NCPU              int    `json:"NCPU"`
	MemTotal          int64  `json:"MemTotal"`
	Driver            string `json:"Driver"`
	DockerRootDir     string `json:"DockerRootDir"`
	Containers        int    `json:"Containers"`
	ContainersRunning int    `json:"ContainersRunning"`
	ContainersPaused  int    `json:"ContainersPaused"`
	ContainersStopped int    `json:"ContainersStopped"`
	Images            int    `json:"Images"`
}

func (c *Client) Version(ctx context.Context) (*Version, error) {
	var out Version
	if err := c.call(ctx, request{method: http.MethodGet, path: "/version"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Info(ctx context.Context) (*Info, error) {
	var out Info
	if err := c.call(ctx, request{method: http.MethodGet, path: "/info"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Event is one entry of GET /events.
type Event struct {
	Type   string `json:"Type"`   // container, image, volume, network, ...
	Action string `json:"Action"` // create, start, die, destroy, pull, ...
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
	Scope    string `json:"scope"`
	Time     int64  `json:"time"`
	TimeNano int64  `json:"timeNano"`
}

// Events streams daemon events until ctx is cancelled or fn returns an error.
func (c *Client) Events(ctx context.Context, filters Filters, fn func(Event) error) error {
	q := url.Values{}
	filters.encode(q)
	resp, err := c.do(ctx, request{method: http.MethodGet, path: "/events", query: q})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	for {
		var event Event
		if err := decoder.Decode(&event); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := fn(event); err != nil {
			return err
		}
	}
}
