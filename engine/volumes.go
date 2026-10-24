package engine

import (
	"context"
	"net/http"
	"net/url"
)

type Volume struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	CreatedAt  string            `json:"CreatedAt"`
	Scope      string            `json:"Scope"`
	Labels     map[string]string `json:"Labels"`
	Options    map[string]string `json:"Options"`
}

type VolumeCreate struct {
	Name       string            `json:"Name,omitempty"`
	Driver     string            `json:"Driver,omitempty"`
	DriverOpts map[string]string `json:"DriverOpts,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
}

func (c *Client) ListVolumes(ctx context.Context, filters Filters) ([]Volume, error) {
	q := url.Values{}
	filters.encode(q)
	var out struct {
		Volumes []Volume `json:"Volumes"`
	}
	err := c.call(ctx, request{method: http.MethodGet, path: "/volumes", query: q}, &out)
	return out.Volumes, err
}

func (c *Client) InspectVolume(ctx context.Context, name string) (*Volume, error) {
	var out Volume
	if err := c.call(ctx, request{method: http.MethodGet, path: "/volumes/" + url.PathEscape(name)}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateVolume(ctx context.Context, spec VolumeCreate) (*Volume, error) {
	var out Volume
	if err := c.call(ctx, request{method: http.MethodPost, path: "/volumes/create", json: spec}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	return c.call(ctx, request{method: http.MethodDelete, path: "/volumes/" + url.PathEscape(name), query: url.Values{"force": {boolString(force)}}}, nil)
}

// PruneVolumes removes unused anonymous volumes, or all unused volumes when
// all is set.
func (c *Client) PruneVolumes(ctx context.Context, all bool) (PruneReport, error) {
	q := url.Values{}
	if all {
		Filters{"all": {"true"}}.encode(q)
	}
	var out struct {
		VolumesDeleted []string
		SpaceReclaimed uint64
	}
	err := c.call(ctx, request{method: http.MethodPost, path: "/volumes/prune", query: q}, &out)
	return PruneReport{Deleted: out.VolumesDeleted, SpaceReclaimed: out.SpaceReclaimed}, err
}
