package api

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lsongdev/docker-go/engine"
	"github.com/lsongdev/docker-go/jobs"
)

type portDTO struct {
	IP       string `json:"ip,omitempty"`
	Private  uint16 `json:"private"`
	Public   uint16 `json:"public,omitempty"`
	Protocol string `json:"protocol"`
}

type containerSummary struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	ImageID  string            `json:"image_id"`
	Command  string            `json:"command"`
	Created  int64             `json:"created"`
	State    string            `json:"state"`
	Status   string            `json:"status"`
	Ports    []portDTO         `json:"ports"`
	Project  string            `json:"project,omitempty"` // compose project
	Service  string            `json:"service,omitempty"` // compose service
	Networks []string          `json:"networks"`
	Labels   map[string]string `json:"labels"`
}

func toContainerSummary(c engine.Container) containerSummary {
	out := containerSummary{
		ID: c.ID, Name: c.Name(), Image: c.Image, ImageID: c.ImageID, Command: c.Command,
		Created: c.Created, State: c.State, Status: c.Status, Labels: c.Labels,
		Project: c.Labels["com.docker.compose.project"], Service: c.Labels["com.docker.compose.service"],
		Ports: []portDTO{}, Networks: []string{},
	}
	seen := map[string]bool{}
	for _, p := range c.Ports {
		// The daemon reports a binding once per address family; show it once.
		key := fmt.Sprintf("%d/%d/%s", p.PrivatePort, p.PublicPort, p.Type)
		if seen[key] {
			continue
		}
		seen[key] = true
		out.Ports = append(out.Ports, portDTO{IP: p.IP, Private: p.PrivatePort, Public: p.PublicPort, Protocol: p.Type})
	}
	slices.SortFunc(out.Ports, func(a, b portDTO) int { return int(a.Private) - int(b.Private) })
	for name := range c.NetworkSettings.Networks {
		out.Networks = append(out.Networks, name)
	}
	slices.Sort(out.Networks)
	return out
}

func (s *Server) listContainers(w http.ResponseWriter, r *http.Request) {
	list, err := s.Engine.ListContainers(r.Context(), engine.ListContainersOptions{All: true})
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]containerSummary, 0, len(list))
	for _, c := range list {
		out = append(out, toContainerSummary(c))
	}
	slices.SortFunc(out, func(a, b containerSummary) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, http.StatusOK, out)
}

type mountDTO struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	RW          bool   `json:"rw"`
}

type endpointDTO struct {
	Network string `json:"network"`
	IP      string `json:"ip,omitempty"`
	IPv6    string `json:"ipv6,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	MAC     string `json:"mac,omitempty"`
}

type portMappingDTO struct {
	Container string `json:"container"` // e.g. 80/tcp
	HostIP    string `json:"host_ip,omitempty"`
	HostPort  string `json:"host_port,omitempty"`
}

type containerDetail struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	ImageID       string            `json:"image_id"`
	Created       time.Time         `json:"created"`
	State         string            `json:"state"`
	Running       bool              `json:"running"`
	Paused        bool              `json:"paused"`
	ExitCode      int               `json:"exit_code"`
	Error         string            `json:"error,omitempty"`
	OOMKilled     bool              `json:"oom_killed"`
	Health        string            `json:"health,omitempty"`
	StartedAt     time.Time         `json:"started_at"`
	FinishedAt    time.Time         `json:"finished_at"`
	RestartCount  int               `json:"restart_count"`
	RestartPolicy string            `json:"restart_policy"`
	Command       []string          `json:"command"`
	Entrypoint    []string          `json:"entrypoint"`
	Cmd           []string          `json:"cmd"`
	Env           []string          `json:"env"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	User          string            `json:"user,omitempty"`
	Hostname      string            `json:"hostname,omitempty"`
	Tty           bool              `json:"tty"`
	Privileged    bool              `json:"privileged"`
	NetworkMode   string            `json:"network_mode,omitempty"`
	Ports         []portMappingDTO  `json:"ports"`
	Mounts        []mountDTO        `json:"mounts"`
	Networks      []endpointDTO     `json:"networks"`
	Labels        map[string]string `json:"labels"`
	Project       string            `json:"project,omitempty"`
}

func toContainerDetail(c *engine.ContainerJSON) containerDetail {
	out := containerDetail{
		ID: c.ID, Name: strings.TrimPrefix(c.Name, "/"), ImageID: c.Image, Created: c.Created,
		RestartCount: c.RestartCount, Command: append([]string{c.Path}, c.Args...),
		Ports: []portMappingDTO{}, Mounts: []mountDTO{}, Networks: []endpointDTO{}, Env: []string{},
	}
	if st := c.State; st != nil {
		out.State, out.Running, out.Paused = st.Status, st.Running, st.Paused
		out.ExitCode, out.Error, out.OOMKilled = st.ExitCode, st.Error, st.OOMKilled
		out.StartedAt, out.FinishedAt = st.StartedAt, st.FinishedAt
		if st.Health != nil {
			out.Health = st.Health.Status
		}
	}
	if cfg := c.Config; cfg != nil {
		out.Image, out.Entrypoint, out.Cmd = cfg.Image, cfg.Entrypoint, cfg.Cmd
		out.WorkingDir, out.User = cfg.WorkingDir, cfg.User
		if cfg.Env != nil {
			out.Env = cfg.Env
		}
		out.Hostname, out.Tty, out.Labels = cfg.Hostname, cfg.Tty, cfg.Labels
		out.Project = cfg.Labels["com.docker.compose.project"]
	}
	if hc := c.HostConfig; hc != nil {
		out.RestartPolicy, out.Privileged, out.NetworkMode = cmp.Or(hc.RestartPolicy.Name, "no"), hc.Privileged, hc.NetworkMode
	}
	if ns := c.NetworkSettings; ns != nil {
		for _, port := range slices.Sorted(maps.Keys(ns.Ports)) {
			bindings := ns.Ports[port]
			if len(bindings) == 0 {
				out.Ports = append(out.Ports, portMappingDTO{Container: port})
			}
			for _, b := range bindings {
				out.Ports = append(out.Ports, portMappingDTO{Container: port, HostIP: b.HostIP, HostPort: b.HostPort})
			}
		}
		for _, name := range slices.Sorted(maps.Keys(ns.Networks)) {
			e := ns.Networks[name]
			out.Networks = append(out.Networks, endpointDTO{Network: name, IP: e.IPAddress, IPv6: e.IPv6Address, Gateway: e.Gateway, MAC: e.MacAddress})
		}
	}
	for _, m := range c.Mounts {
		out.Mounts = append(out.Mounts, mountDTO{Type: m.Type, Name: m.Name, Source: m.Source, Destination: m.Destination, Mode: m.Mode, RW: m.RW})
	}
	return out
}

func (s *Server) getContainer(w http.ResponseWriter, r *http.Request) {
	c, err := s.Engine.InspectContainer(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toContainerDetail(c))
}

// createContainerRequest is a friendly subset of the Engine's create body.
type createContainerRequest struct {
	Name       string   `json:"name"`
	Image      string   `json:"image"`
	Command    string   `json:"command"`    // shell-style words, overrides CMD
	Entrypoint string   `json:"entrypoint"` // shell-style words
	Env        []string `json:"env"`        // KEY=VALUE
	Ports      []string `json:"ports"`      // [ip:]host:container[/proto] or container[/proto]
	Volumes    []string `json:"volumes"`    // source:target[:ro]
	Network    string   `json:"network"`
	Restart    string   `json:"restart"` // no, always, unless-stopped, on-failure
	Labels     []string `json:"labels"`  // KEY=VALUE
	WorkingDir string   `json:"working_dir"`
	User       string   `json:"user"`
	Tty        bool     `json:"tty"`
	Privileged bool     `json:"privileged"`
	Pull       string   `json:"pull"` // missing (default), always, never
	Start      bool     `json:"start"`
}

func (req createContainerRequest) spec() (engine.ContainerCreate, error) {
	var spec engine.ContainerCreate
	image := strings.TrimSpace(req.Image)
	if image == "" {
		return spec, invalid("image is required")
	}
	if req.Name != "" && !validContainerName(req.Name) {
		return spec, invalid("container name may only contain letters, digits, '_', '.' and '-'")
	}
	var err error
	if spec.Cmd, err = splitWords(req.Command); err != nil {
		return spec, invalid("command: %v", err)
	}
	if spec.Entrypoint, err = splitWords(req.Entrypoint); err != nil {
		return spec, invalid("entrypoint: %v", err)
	}
	spec.Image, spec.WorkingDir, spec.User, spec.Tty, spec.OpenStdin = image, req.WorkingDir, req.User, req.Tty, req.Tty
	for _, e := range req.Env {
		if e = strings.TrimSpace(e); e == "" {
			continue
		}
		if !strings.Contains(e, "=") || strings.HasPrefix(e, "=") {
			return spec, invalid("env %q must be KEY=VALUE", e)
		}
		spec.Env = append(spec.Env, e)
	}
	for _, l := range req.Labels {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		key, value, _ := strings.Cut(l, "=")
		if spec.Labels == nil {
			spec.Labels = map[string]string{}
		}
		spec.Labels[key] = value
	}
	host := &engine.HostConfig{Privileged: req.Privileged, NetworkMode: req.Network}
	switch req.Restart {
	case "", "no":
	case "always", "unless-stopped", "on-failure":
		host.RestartPolicy.Name = req.Restart
	default:
		return spec, invalid("unknown restart policy %q", req.Restart)
	}
	for _, p := range req.Ports {
		if p = strings.TrimSpace(p); p == "" {
			continue
		}
		port, binding, err := parsePortSpec(p)
		if err != nil {
			return spec, err
		}
		if spec.ExposedPorts == nil {
			spec.ExposedPorts, host.PortBindings = map[string]struct{}{}, map[string][]engine.PortBinding{}
		}
		spec.ExposedPorts[port] = struct{}{}
		if binding != nil {
			host.PortBindings[port] = append(host.PortBindings[port], *binding)
		}
	}
	for _, v := range req.Volumes {
		if v = strings.TrimSpace(v); v == "" {
			continue
		}
		parts := strings.Split(v, ":")
		if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || !strings.HasPrefix(parts[1], "/") {
			return spec, invalid("volume %q must be source:/container/path[:ro]", v)
		}
		if len(parts) == 3 && parts[2] != "ro" && parts[2] != "rw" {
			return spec, invalid("volume %q: mode must be ro or rw", v)
		}
		host.Binds = append(host.Binds, v)
	}
	spec.HostConfig = host
	return spec, nil
}

func validContainerName(name string) bool {
	for i, c := range name {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || i > 0 && (c == '_' || c == '.' || c == '-')
		if !ok {
			return false
		}
	}
	return name != ""
}

// parsePortSpec parses docker-style port specs: "80", "80/udp",
// "8080:80", "127.0.0.1:8080:80/tcp", "[::1]:8080:80".
func parsePortSpec(spec string) (string, *engine.PortBinding, error) {
	proto := "tcp"
	if i := strings.LastIndex(spec, "/"); i >= 0 {
		proto = strings.ToLower(spec[i+1:])
		spec = spec[:i]
		if proto != "tcp" && proto != "udp" && proto != "sctp" {
			return "", nil, invalid("port protocol must be tcp, udp or sctp")
		}
	}
	var hostIP, hostPort, containerPort string
	if strings.HasPrefix(spec, "[") { // bracketed IPv6 host address
		end := strings.Index(spec, "]:")
		if end < 0 {
			return "", nil, invalid("invalid port %q", spec)
		}
		hostIP, spec = spec[1:end], spec[end+2:]
		var ok bool
		if hostPort, containerPort, ok = strings.Cut(spec, ":"); !ok {
			return "", nil, invalid("invalid port %q", spec)
		}
	} else {
		switch parts := strings.Split(spec, ":"); len(parts) {
		case 1:
			containerPort = parts[0]
		case 2:
			hostPort, containerPort = parts[0], parts[1]
		case 3:
			hostIP, hostPort, containerPort = parts[0], parts[1], parts[2]
		default:
			return "", nil, invalid("invalid port %q", spec)
		}
	}
	if !validPort(containerPort) || hostPort != "" && !validPort(hostPort) {
		return "", nil, invalid("invalid port %q", spec)
	}
	if hostIP != "" && net.ParseIP(hostIP) == nil {
		return "", nil, invalid("invalid host IP %q", hostIP)
	}
	port := containerPort + "/" + proto
	if hostPort == "" && hostIP == "" && !strings.Contains(spec, ":") {
		return port, nil, nil // expose only
	}
	return port, &engine.PortBinding{HostIP: hostIP, HostPort: hostPort}, nil
}

func validPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n > 0 && n <= 65535
}

// splitWords splits a command line into words, honouring single quotes,
// double quotes and backslash escapes. It does not expand variables.
func splitWords(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord, quote, escaped := false, rune(0), false
	for _, c := range s {
		switch {
		case escaped:
			cur.WriteRune(c)
			escaped = false
		case c == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(c)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("unterminated quote or escape")
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

func (s *Server) createContainer(w http.ResponseWriter, r *http.Request) {
	var req createContainerRequest
	if err := decode(w, r, &req); err != nil {
		fail(w, err)
		return
	}
	spec, err := req.spec()
	if err != nil {
		fail(w, err)
		return
	}
	switch req.Pull {
	case "", "missing", "always", "never":
	default:
		fail(w, invalid("pull must be missing, always or never"))
		return
	}
	job := jobs.Spec{Kind: "container.create", Title: "Create container " + cmp.Or(req.Name, spec.Image)}
	if req.Name != "" {
		job.Target = "container:" + req.Name
	}
	s.startJob(w, job, func(ctx context.Context, log func(string)) error {
		if err := s.ensureImage(ctx, spec.Image, req.Pull, log); err != nil {
			return err
		}
		id, err := s.Engine.CreateContainer(ctx, req.Name, spec)
		if err != nil {
			return err
		}
		log("Created " + id[:12])
		if req.Start {
			if err := s.Engine.StartContainer(ctx, id); err != nil {
				return err
			}
			log("Started")
		}
		return nil
	})
}

// ensureImage pulls image according to policy (missing, always, never).
func (s *Server) ensureImage(ctx context.Context, image, policy string, log func(string)) error {
	if policy == "never" {
		return nil
	}
	if policy != "always" {
		if _, err := s.Engine.InspectImage(ctx, image); err == nil {
			return nil
		} else if !engine.IsNotFound(err) {
			return err
		}
	}
	log("Pulling " + image)
	return s.Engine.PullImage(ctx, image, engine.PullOptions{}, progressLogger(log))
}

// progressLogger logs pull/build messages, skipping per-chunk download
// progress, which would flood the log.
func progressLogger(log func(string)) func(engine.Message) error {
	return func(m engine.Message) error {
		if m.Progress != "" && (m.Status == "Downloading" || m.Status == "Extracting") {
			return nil
		}
		if text := m.Text(); text != "" {
			log(text)
		}
		return nil
	}
}

func (s *Server) containerAction(w http.ResponseWriter, r *http.Request) {
	id, ctx := r.PathValue("id"), r.Context()
	var err error
	switch r.PathValue("action") {
	case "start":
		err = s.Engine.StartContainer(ctx, id)
	case "stop":
		err = s.Engine.StopContainer(ctx, id, nil)
	case "restart":
		err = s.Engine.RestartContainer(ctx, id, nil)
	case "kill":
		err = s.Engine.KillContainer(ctx, id, "")
	case "pause":
		err = s.Engine.PauseContainer(ctx, id)
	case "unpause":
		err = s.Engine.UnpauseContainer(ctx, id)
	case "rename":
		var body struct {
			Name string `json:"name"`
		}
		if err = decode(w, r, &body); err == nil {
			if !validContainerName(body.Name) {
				err = invalid("invalid container name")
			} else {
				err = s.Engine.RenameContainer(ctx, id, body.Name)
			}
		}
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown container action")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeContainer(w http.ResponseWriter, r *http.Request) {
	opts := engine.RemoveContainerOptions{Force: queryBool(r, "force"), RemoveVolumes: queryBool(r, "volumes")}
	if err := s.Engine.RemoveContainer(r.Context(), r.PathValue("id"), opts); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pruneContainers(w http.ResponseWriter, r *http.Request) {
	report, err := s.Engine.PruneContainers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// containerLogs returns recent log lines as JSON, or streams them as SSE
// ("line" events) when follow=1.
func (s *Server) containerLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tail := cmp.Or(q.Get("tail"), "500")
	if tail != "all" {
		if n, err := strconv.Atoi(tail); err != nil || n < 0 {
			fail(w, invalid("tail must be a number or 'all'"))
			return
		}
	}
	opts := engine.LogsOptions{Tail: tail, Timestamps: queryBool(r, "timestamps"), Follow: queryBool(r, "follow")}
	id := r.PathValue("id")
	if !opts.Follow {
		lines := []engine.LogLine{}
		err := s.Engine.Logs(r.Context(), id, opts, func(l engine.LogLine) error {
			lines = append(lines, l)
			return nil
		})
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, lines)
		return
	}
	// Check the container exists before committing to a stream.
	if _, err := s.Engine.InspectContainer(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	stream := newSSE(w)
	err := s.Engine.Logs(r.Context(), id, opts, func(l engine.LogLine) error { return stream.send("line", l) })
	if err != nil && r.Context().Err() == nil {
		stream.send("error", map[string]string{"message": err.Error()})
	}
	stream.send("end", nil)
}

// containerStats streams resource usage as SSE "stats" events.
func (s *Server) containerStats(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Engine.InspectContainer(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	stream := newSSE(w)
	err := s.Engine.Stats(r.Context(), id, func(st engine.Stats) error { return stream.send("stats", st) })
	if err != nil && r.Context().Err() == nil {
		stream.send("error", map[string]string{"message": err.Error()})
	}
}
