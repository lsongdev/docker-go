package api

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lsongdev/docker-go/engine"
	"github.com/lsongdev/docker-go/jobs"
)

type imageSummary struct {
	ID         string   `json:"id"`
	Tags       []string `json:"tags"`
	Digests    []string `json:"digests"`
	Created    int64    `json:"created"`
	Size       int64    `json:"size"`
	Dangling   bool     `json:"dangling"`
	Containers []string `json:"containers"` // names of containers using it
}

// imageUsers maps image IDs to the names of containers using them.
func (s *Server) imageUsers(ctx context.Context) (map[string][]string, error) {
	containers, err := s.Engine.ListContainers(ctx, engine.ListContainersOptions{All: true})
	if err != nil {
		return nil, err
	}
	users := map[string][]string{}
	for _, c := range containers {
		users[c.ImageID] = append(users[c.ImageID], c.Name())
	}
	return users, nil
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	images, err := s.Engine.ListImages(r.Context(), engine.ListImagesOptions{})
	if err != nil {
		fail(w, err)
		return
	}
	users, err := s.imageUsers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]imageSummary, 0, len(images))
	for _, img := range images {
		tags := slices.DeleteFunc(slices.Clone(img.RepoTags), func(t string) bool { return t == "<none>:<none>" })
		out = append(out, imageSummary{
			ID: img.ID, Tags: nonNil(tags), Digests: nonNil(img.RepoDigests), Created: img.Created,
			Size: img.Size, Dangling: len(tags) == 0, Containers: nonNil(users[img.ID]),
		})
	}
	slices.SortFunc(out, func(a, b imageSummary) int { return cmp.Compare(b.Created, a.Created) })
	writeJSON(w, http.StatusOK, out)
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

type historyDTO struct {
	Created   int64    `json:"created"`
	CreatedBy string   `json:"created_by"`
	Size      int64    `json:"size"`
	Tags      []string `json:"tags"`
	Comment   string   `json:"comment,omitempty"`
}

type imageDetail struct {
	ID           string            `json:"id"`
	Tags         []string          `json:"tags"`
	Digests      []string          `json:"digests"`
	Created      time.Time         `json:"created"`
	Author       string            `json:"author,omitempty"`
	Architecture string            `json:"architecture"`
	Os           string            `json:"os"`
	Size         int64             `json:"size"`
	Env          []string          `json:"env"`
	Cmd          []string          `json:"cmd"`
	Entrypoint   []string          `json:"entrypoint"`
	WorkingDir   string            `json:"working_dir,omitempty"`
	User         string            `json:"user,omitempty"`
	ExposedPorts []string          `json:"exposed_ports"`
	Volumes      []string          `json:"volumes"`
	Labels       map[string]string `json:"labels"`
	Layers       int               `json:"layers"`
	History      []historyDTO      `json:"history"`
	Containers   []string          `json:"containers"`
}

func (s *Server) getImage(w http.ResponseWriter, r *http.Request) {
	ctx, id := r.Context(), r.PathValue("id")
	img, err := s.Engine.InspectImage(ctx, id)
	if err != nil {
		fail(w, err)
		return
	}
	history, err := s.Engine.ImageHistory(ctx, img.ID)
	if err != nil {
		fail(w, err)
		return
	}
	users, err := s.imageUsers(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	out := imageDetail{
		ID: img.ID, Tags: nonNil(img.RepoTags), Digests: nonNil(img.RepoDigests), Created: img.Created,
		Author: img.Author, Architecture: img.Architecture, Os: img.Os, Size: img.Size,
		Layers: len(img.RootFS.Layers), Env: []string{}, ExposedPorts: []string{}, Volumes: []string{},
		History: []historyDTO{}, Containers: nonNil(users[img.ID]),
	}
	if cfg := img.Config; cfg != nil {
		out.Cmd, out.Entrypoint, out.WorkingDir, out.User, out.Labels = cfg.Cmd, cfg.Entrypoint, cfg.WorkingDir, cfg.User, cfg.Labels
		out.Env = nonNil(cfg.Env)
		out.ExposedPorts = nonNil(slices.Sorted(maps.Keys(cfg.ExposedPorts)))
		out.Volumes = nonNil(slices.Sorted(maps.Keys(cfg.Volumes)))
	}
	for _, h := range history {
		out.History = append(out.History, historyDTO{Created: h.Created, CreatedBy: h.CreatedBy, Size: h.Size, Tags: nonNil(h.Tags), Comment: h.Comment})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) removeImage(w http.ResponseWriter, r *http.Request) {
	items, err := s.Engine.RemoveImage(r.Context(), r.PathValue("id"), engine.RemoveImageOptions{Force: queryBool(r, "force")})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNil(items))
}

func (s *Server) pruneImages(w http.ResponseWriter, r *http.Request) {
	report, err := s.Engine.PruneImages(r.Context(), queryBool(r, "all"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

type pullRequest struct {
	Image    string `json:"image"`
	Platform string `json:"platform"`
}

func (s *Server) pullImage(w http.ResponseWriter, r *http.Request) {
	var req pullRequest
	if err := decode(w, r, &req); err != nil {
		fail(w, err)
		return
	}
	image := strings.TrimSpace(req.Image)
	if image == "" || strings.ContainsAny(image, " \t\n") {
		fail(w, invalid("a valid image reference is required"))
		return
	}
	repo, tag := engine.ParseReference(image)
	ref := repo + ":" + tag
	if strings.Contains(image, "@") {
		ref = repo + "@" + tag
	}
	s.startJob(w, jobs.Spec{Kind: "image.pull", Target: "image:" + ref, Title: "Pull " + ref}, func(ctx context.Context, log func(string)) error {
		if err := s.Engine.PullImage(ctx, ref, engine.PullOptions{Platform: req.Platform}, progressLogger(log)); err != nil {
			return err
		}
		log("Pulled " + ref)
		return nil
	})
}

// buildRequest builds from an inline Dockerfile, a directory on the server,
// or a git repository.
type buildRequest struct {
	Tag        string            `json:"tag"`
	Source     string            `json:"source"`     // dockerfile, directory, git
	Dockerfile string            `json:"dockerfile"` // content (dockerfile) or path in context
	Directory  string            `json:"directory"`
	Repository string            `json:"repository"`
	Ref        string            `json:"ref"` // branch or tag
	BuildArgs  map[string]string `json:"build_args"`
	Pull       bool              `json:"pull"`
	NoCache    bool              `json:"no_cache"`
}

func (s *Server) buildImage(w http.ResponseWriter, r *http.Request) {
	var req buildRequest
	if err := decode(w, r, &req); err != nil {
		fail(w, err)
		return
	}
	req.Tag = strings.TrimSpace(req.Tag)
	if req.Tag == "" || strings.ContainsAny(req.Tag, " \t\n") {
		fail(w, invalid("a valid image tag is required"))
		return
	}
	switch req.Source {
	case "dockerfile":
		if strings.TrimSpace(req.Dockerfile) == "" {
			fail(w, invalid("Dockerfile content is required"))
			return
		}
	case "directory":
		if !filepath.IsAbs(req.Directory) {
			fail(w, invalid("directory must be an absolute path on the server"))
			return
		}
		if info, err := os.Stat(req.Directory); err != nil || !info.IsDir() {
			fail(w, invalid("directory does not exist"))
			return
		}
	case "git":
		if !strings.HasPrefix(req.Repository, "https://") && !strings.HasPrefix(req.Repository, "git@") && !strings.HasPrefix(req.Repository, "ssh://") {
			fail(w, invalid("repository must be an https, ssh or git@ URL"))
			return
		}
	default:
		fail(w, invalid("source must be dockerfile, directory or git"))
		return
	}
	s.startJob(w, jobs.Spec{Kind: "image.build", Target: "build:" + req.Tag, Title: "Build " + req.Tag}, func(ctx context.Context, log func(string)) error {
		buildCtx, dockerfile, cleanup, err := prepareBuildContext(ctx, req, log)
		if err != nil {
			return err
		}
		defer cleanup()
		opts := engine.BuildOptions{Tags: []string{req.Tag}, Dockerfile: dockerfile, BuildArgs: req.BuildArgs, Pull: req.Pull, NoCache: req.NoCache}
		if err := s.Engine.BuildImage(ctx, buildCtx, opts, progressLogger(log)); err != nil {
			return err
		}
		log("Built " + req.Tag)
		return nil
	})
}

// prepareBuildContext returns the tar context and the Dockerfile path in it.
func prepareBuildContext(ctx context.Context, req buildRequest, log func(string)) (io.Reader, string, func(), error) {
	noop := func() {}
	switch req.Source {
	case "dockerfile":
		r, err := engine.TarFiles(map[string][]byte{"Dockerfile": []byte(req.Dockerfile)})
		return r, "Dockerfile", noop, err
	case "directory":
		rc := engine.TarDirectory(req.Directory)
		return rc, cmp.Or(req.Dockerfile, "Dockerfile"), func() { rc.Close() }, nil
	}
	dir, err := os.MkdirTemp("", "docker-go-build-")
	if err != nil {
		return nil, "", noop, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	args := []string{"clone", "--depth", "1"}
	if req.Ref != "" {
		args = append(args, "--branch", req.Ref)
	}
	args = append(args, "--", req.Repository, dir)
	log("Cloning " + req.Repository)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return nil, "", noop, fmt.Errorf("git clone: %s", strings.TrimSpace(string(out)))
	}
	rc := engine.TarDirectory(dir)
	return rc, cmp.Or(req.Dockerfile, "Dockerfile"), func() { rc.Close(); cleanup() }, nil
}
