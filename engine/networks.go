package engine

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

type Network struct {
	ID         string                      `json:"Id"`
	Name       string                      `json:"Name"`
	Created    time.Time                   `json:"Created"`
	Scope      string                      `json:"Scope"`
	Driver     string                      `json:"Driver"`
	EnableIPv6 bool                        `json:"EnableIPv6"`
	Internal   bool                        `json:"Internal"`
	Attachable bool                        `json:"Attachable"`
	IPAM       IPAM                        `json:"IPAM"`
	Containers map[string]NetworkContainer `json:"Containers"`
	Options    map[string]string           `json:"Options"`
	Labels     map[string]string           `json:"Labels"`
}

type IPAM struct {
	Driver string       `json:"Driver,omitempty"`
	Config []IPAMConfig `json:"Config,omitempty"`
}

type IPAMConfig struct {
	Subnet  string `json:"Subnet,omitempty"`
	IPRange string `json:"IPRange,omitempty"`
	Gateway string `json:"Gateway,omitempty"`
}

type NetworkContainer struct {
	Name        string `json:"Name"`
	EndpointID  string `json:"EndpointID"`
	MacAddress  string `json:"MacAddress"`
	IPv4Address string `json:"IPv4Address"`
	IPv6Address string `json:"IPv6Address"`
}

type NetworkCreate struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver,omitempty"`
	Internal   bool              `json:"Internal,omitempty"`
	Attachable bool              `json:"Attachable,omitempty"`
	EnableIPv6 bool              `json:"EnableIPv6,omitempty"`
	IPAM       *IPAM             `json:"IPAM,omitempty"`
	Options    map[string]string `json:"Options,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
}

func (c *Client) ListNetworks(ctx context.Context, filters Filters) ([]Network, error) {
	q := url.Values{}
	filters.encode(q)
	var out []Network
	err := c.call(ctx, request{method: http.MethodGet, path: "/networks", query: q}, &out)
	return out, err
}

// InspectNetwork returns a network including its attached containers.
func (c *Client) InspectNetwork(ctx context.Context, id string) (*Network, error) {
	var out Network
	if err := c.call(ctx, request{method: http.MethodGet, path: "/networks/" + url.PathEscape(id)}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateNetwork creates a network and returns its ID.
func (c *Client) CreateNetwork(ctx context.Context, spec NetworkCreate) (string, error) {
	var out struct {
		ID string `json:"Id"`
	}
	err := c.call(ctx, request{method: http.MethodPost, path: "/networks/create", json: spec}, &out)
	return out.ID, err
}

func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.call(ctx, request{method: http.MethodDelete, path: "/networks/" + url.PathEscape(id)}, nil)
}

func (c *Client) ConnectNetwork(ctx context.Context, networkID, containerID string, endpoint *EndpointSettings) error {
	body := map[string]any{"Container": containerID}
	if endpoint != nil {
		body["EndpointConfig"] = endpoint
	}
	return c.call(ctx, request{method: http.MethodPost, path: "/networks/" + url.PathEscape(networkID) + "/connect", json: body}, nil)
}

func (c *Client) DisconnectNetwork(ctx context.Context, networkID, containerID string, force bool) error {
	body := map[string]any{"Container": containerID, "Force": force}
	return c.call(ctx, request{method: http.MethodPost, path: "/networks/" + url.PathEscape(networkID) + "/disconnect", json: body}, nil)
}

func (c *Client) PruneNetworks(ctx context.Context) (PruneReport, error) {
	var out struct {
		NetworksDeleted []string
	}
	err := c.call(ctx, request{method: http.MethodPost, path: "/networks/prune"}, &out)
	return PruneReport{Deleted: out.NetworksDeleted}, err
}
