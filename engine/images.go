package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ImageSummary is an entry of GET /images/json.
type ImageSummary struct {
	ID          string            `json:"Id"`
	ParentID    string            `json:"ParentId"`
	RepoTags    []string          `json:"RepoTags"`
	RepoDigests []string          `json:"RepoDigests"`
	Created     int64             `json:"Created"`
	Size        int64             `json:"Size"`
	Containers  int64             `json:"Containers"`
	Labels      map[string]string `json:"Labels"`
}

// ImageInspect is the response of GET /images/{name}/json.
type ImageInspect struct {
	ID           string           `json:"Id"`
	RepoTags     []string         `json:"RepoTags"`
	RepoDigests  []string         `json:"RepoDigests"`
	Created      time.Time        `json:"Created"`
	Author       string           `json:"Author"`
	Architecture string           `json:"Architecture"`
	Variant      string           `json:"Variant"`
	Os           string           `json:"Os"`
	Size         int64            `json:"Size"`
	Config       *ContainerConfig `json:"Config"`
	RootFS       struct {
		Layers []string `json:"Layers"`
	} `json:"RootFS"`
}

// HistoryEntry is one layer of GET /images/{name}/history.
type HistoryEntry struct {
	ID        string   `json:"Id"`
	Created   int64    `json:"Created"`
	CreatedBy string   `json:"CreatedBy"`
	Tags      []string `json:"Tags"`
	Size      int64    `json:"Size"`
	Comment   string   `json:"Comment"`
}

type ListImagesOptions struct {
	All     bool // include intermediate images
	Filters Filters
}

func (c *Client) ListImages(ctx context.Context, opts ListImagesOptions) ([]ImageSummary, error) {
	q := url.Values{}
	if opts.All {
		q.Set("all", "1")
	}
	opts.Filters.encode(q)
	var out []ImageSummary
	err := c.call(ctx, request{method: http.MethodGet, path: "/images/json", query: q}, &out)
	return out, err
}

func (c *Client) InspectImage(ctx context.Context, name string) (*ImageInspect, error) {
	var out ImageInspect
	if err := c.call(ctx, request{method: http.MethodGet, path: "/images/" + name + "/json"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ImageHistory(ctx context.Context, name string) ([]HistoryEntry, error) {
	var out []HistoryEntry
	err := c.call(ctx, request{method: http.MethodGet, path: "/images/" + name + "/history"}, &out)
	return out, err
}

// ParseReference splits an image reference into repository and tag (or
// digest). A reference without either defaults to the "latest" tag.
func ParseReference(ref string) (repo, tag string) {
	ref = strings.TrimSpace(ref)
	if i := strings.Index(ref, "@"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	// A colon after the last slash separates the tag; one before it belongs
	// to a registry host:port.
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		return ref[:i], ref[i+1:]
	}
	return ref, "latest"
}

// RegistryAuth holds credentials for a private registry.
type RegistryAuth struct {
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	ServerAddress string `json:"serveraddress,omitempty"`
}

func (a *RegistryAuth) header() http.Header {
	if a == nil {
		return nil
	}
	data, _ := json.Marshal(a)
	return http.Header{"X-Registry-Auth": {base64.URLEncoding.EncodeToString(data)}}
}

type PullOptions struct {
	Platform string // e.g. linux/arm64; empty uses the daemon's platform
	Auth     *RegistryAuth
}

// PullImage pulls ref, reporting progress messages to fn.
func (c *Client) PullImage(ctx context.Context, ref string, opts PullOptions, fn func(Message) error) error {
	repo, tag := ParseReference(ref)
	q := url.Values{"fromImage": {repo}, "tag": {tag}}
	if opts.Platform != "" {
		q.Set("platform", opts.Platform)
	}
	resp, err := c.do(ctx, request{method: http.MethodPost, path: "/images/create", query: q, header: opts.Auth.header()})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return readMessages(resp.Body, fn)
}

func (c *Client) TagImage(ctx context.Context, source, target string) error {
	repo, tag := ParseReference(target)
	return c.call(ctx, request{method: http.MethodPost, path: "/images/" + source + "/tag", query: url.Values{"repo": {repo}, "tag": {tag}}}, nil)
}

type RemoveImageOptions struct {
	Force   bool
	NoPrune bool
}

// ImageDeleteItem reports one untagged reference or deleted layer.
type ImageDeleteItem struct {
	Untagged string `json:"Untagged,omitempty"`
	Deleted  string `json:"Deleted,omitempty"`
}

func (c *Client) RemoveImage(ctx context.Context, name string, opts RemoveImageOptions) ([]ImageDeleteItem, error) {
	q := url.Values{"force": {boolString(opts.Force)}, "noprune": {boolString(opts.NoPrune)}}
	var out []ImageDeleteItem
	err := c.call(ctx, request{method: http.MethodDelete, path: "/images/" + name, query: q}, &out)
	return out, err
}

// PruneImages removes dangling images, or every unused image when all is set.
func (c *Client) PruneImages(ctx context.Context, all bool) (PruneReport, error) {
	q := url.Values{}
	if all {
		Filters{"dangling": {"false"}}.encode(q)
	}
	var out struct {
		ImagesDeleted  []ImageDeleteItem
		SpaceReclaimed uint64
	}
	err := c.call(ctx, request{method: http.MethodPost, path: "/images/prune", query: q}, &out)
	report := PruneReport{SpaceReclaimed: out.SpaceReclaimed}
	for _, item := range out.ImagesDeleted {
		if item.Deleted != "" {
			report.Deleted = append(report.Deleted, item.Deleted)
		}
	}
	return report, err
}

type BuildOptions struct {
	Tags       []string
	Dockerfile string // path inside the context; default "Dockerfile"
	BuildArgs  map[string]string
	Labels     map[string]string
	Target     string
	Platform   string
	Pull       bool // always attempt to pull newer base images
	NoCache    bool
}

// BuildImage builds an image from a tar build context, reporting build output
// to fn.
func (c *Client) BuildImage(ctx context.Context, buildContext io.Reader, opts BuildOptions, fn func(Message) error) error {
	q := url.Values{"rm": {"1"}, "forcerm": {"1"}}
	for _, tag := range opts.Tags {
		q.Add("t", tag)
	}
	if opts.Dockerfile != "" {
		q.Set("dockerfile", opts.Dockerfile)
	}
	if opts.Target != "" {
		q.Set("target", opts.Target)
	}
	if opts.Platform != "" {
		q.Set("platform", opts.Platform)
	}
	if opts.Pull {
		q.Set("pull", "1")
	}
	if opts.NoCache {
		q.Set("nocache", "1")
	}
	if len(opts.BuildArgs) > 0 {
		data, _ := json.Marshal(opts.BuildArgs)
		q.Set("buildargs", string(data))
	}
	if len(opts.Labels) > 0 {
		data, _ := json.Marshal(opts.Labels)
		q.Set("labels", string(data))
	}
	resp, err := c.do(ctx, request{method: http.MethodPost, path: "/build", query: q, body: buildContext, header: http.Header{"Content-Type": {"application/x-tar"}}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return readMessages(resp.Body, fn)
}
