package api

import (
	"cmp"
	"context"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/lsongdev/docker-go/engine"
)

type volumeDTO struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	CreatedAt  string            `json:"created_at"`
	Scope      string            `json:"scope"`
	Labels     map[string]string `json:"labels"`
	Options    map[string]string `json:"options"`
	Project    string            `json:"project,omitempty"`
	Containers []string          `json:"containers"` // names of containers mounting it
}

// volumeUsers maps volume names to the containers that mount them.
func (s *Server) volumeUsers(ctx context.Context) (map[string][]string, error) {
	containers, err := s.Engine.ListContainers(ctx, engine.ListContainersOptions{All: true})
	if err != nil {
		return nil, err
	}
	users := map[string][]string{}
	for _, c := range containers {
		for _, m := range c.Mounts {
			if m.Type == "volume" && m.Name != "" {
				users[m.Name] = append(users[m.Name], c.Name())
			}
		}
	}
	return users, nil
}

func toVolumeDTO(v engine.Volume, users map[string][]string) volumeDTO {
	return volumeDTO{
		Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint, CreatedAt: v.CreatedAt, Scope: v.Scope,
		Labels: v.Labels, Options: v.Options, Project: v.Labels["com.docker.compose.project"], Containers: nonNil(users[v.Name]),
	}
}

func (s *Server) listVolumes(w http.ResponseWriter, r *http.Request) {
	volumes, err := s.Engine.ListVolumes(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	users, err := s.volumeUsers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	out := make([]volumeDTO, 0, len(volumes))
	for _, v := range volumes {
		out = append(out, toVolumeDTO(v, users))
	}
	slices.SortFunc(out, func(a, b volumeDTO) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getVolume(w http.ResponseWriter, r *http.Request) {
	v, err := s.Engine.InspectVolume(r.Context(), r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	users, err := s.volumeUsers(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toVolumeDTO(*v, users))
}

type createVolumeRequest struct {
	Name    string            `json:"name"`
	Driver  string            `json:"driver"`
	Options map[string]string `json:"options"`
	Labels  map[string]string `json:"labels"`
}

func (s *Server) createVolume(w http.ResponseWriter, r *http.Request) {
	var req createVolumeRequest
	if err := decode(w, r, &req); err != nil {
		fail(w, err)
		return
	}
	if req.Name != "" && !validContainerName(req.Name) {
		fail(w, invalid("volume name may only contain letters, digits, '_', '.' and '-'"))
		return
	}
	v, err := s.Engine.CreateVolume(r.Context(), engine.VolumeCreate{Name: req.Name, Driver: cmp.Or(req.Driver, "local"), DriverOpts: req.Options, Labels: req.Labels})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toVolumeDTO(*v, nil))
}

func (s *Server) removeVolume(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.RemoveVolume(r.Context(), r.PathValue("name"), queryBool(r, "force")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pruneVolumes(w http.ResponseWriter, r *http.Request) {
	report, err := s.Engine.PruneVolumes(r.Context(), queryBool(r, "all"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

type subnetDTO struct {
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway,omitempty"`
	IPRange string `json:"ip_range,omitempty"`
}

type networkContainerDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	IPv4 string `json:"ipv4,omitempty"`
	IPv6 string `json:"ipv6,omitempty"`
	MAC  string `json:"mac,omitempty"`
}

type networkDTO struct {
	ID         string                `json:"id"`
	Name       string                `json:"name"`
	Driver     string                `json:"driver"`
	Scope      string                `json:"scope"`
	Created    time.Time             `json:"created"`
	Internal   bool                  `json:"internal"`
	Attachable bool                  `json:"attachable"`
	IPv6       bool                  `json:"ipv6"`
	Builtin    bool                  `json:"builtin"` // bridge, host, none: cannot be removed
	Subnets    []subnetDTO           `json:"subnets"`
	Labels     map[string]string     `json:"labels"`
	Options    map[string]string     `json:"options"`
	Project    string                `json:"project,omitempty"`
	Containers []networkContainerDTO `json:"containers"`
}

func toNetworkDTO(n engine.Network) networkDTO {
	out := networkDTO{
		ID: n.ID, Name: n.Name, Driver: n.Driver, Scope: n.Scope, Created: n.Created, Internal: n.Internal,
		Attachable: n.Attachable, IPv6: n.EnableIPv6, Labels: n.Labels, Options: n.Options,
		Builtin: n.Name == "bridge" || n.Name == "host" || n.Name == "none",
		Project: n.Labels["com.docker.compose.project"], Subnets: []subnetDTO{}, Containers: []networkContainerDTO{},
	}
	for _, c := range n.IPAM.Config {
		out.Subnets = append(out.Subnets, subnetDTO{Subnet: c.Subnet, Gateway: c.Gateway, IPRange: c.IPRange})
	}
	for id, c := range n.Containers {
		out.Containers = append(out.Containers, networkContainerDTO{ID: id, Name: c.Name, IPv4: c.IPv4Address, IPv6: c.IPv6Address, MAC: c.MacAddress})
	}
	slices.SortFunc(out.Containers, func(a, b networkContainerDTO) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func (s *Server) listNetworks(w http.ResponseWriter, r *http.Request) {
	networks, err := s.Engine.ListNetworks(r.Context(), nil)
	if err != nil {
		fail(w, err)
		return
	}
	// The list endpoint omits attached containers; count them from the
	// container side instead of inspecting every network.
	containers, err := s.Engine.ListContainers(r.Context(), engine.ListContainersOptions{All: true})
	if err != nil {
		fail(w, err)
		return
	}
	attached := map[string][]networkContainerDTO{}
	for _, c := range containers {
		for _, e := range c.NetworkSettings.Networks {
			attached[e.NetworkID] = append(attached[e.NetworkID], networkContainerDTO{ID: c.ID, Name: c.Name(), IPv4: e.IPAddress, MAC: e.MacAddress})
		}
	}
	out := make([]networkDTO, 0, len(networks))
	for _, n := range networks {
		dto := toNetworkDTO(n)
		if len(dto.Containers) == 0 {
			dto.Containers = nonNil(attached[n.ID])
		}
		out = append(out, dto)
	}
	slices.SortFunc(out, func(a, b networkDTO) int { return strings.Compare(a.Name, b.Name) })
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getNetwork(w http.ResponseWriter, r *http.Request) {
	n, err := s.Engine.InspectNetwork(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNetworkDTO(*n))
}

type createNetworkRequest struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Subnet     string            `json:"subnet"`
	Gateway    string            `json:"gateway"`
	IPRange    string            `json:"ip_range"`
	Internal   bool              `json:"internal"`
	Attachable bool              `json:"attachable"`
	IPv6       bool              `json:"ipv6"`
	Labels     map[string]string `json:"labels"`
}

func (s *Server) createNetwork(w http.ResponseWriter, r *http.Request) {
	var req createNetworkRequest
	if err := decode(w, r, &req); err != nil {
		fail(w, err)
		return
	}
	if !validContainerName(req.Name) {
		fail(w, invalid("network name may only contain letters, digits, '_', '.' and '-'"))
		return
	}
	spec := engine.NetworkCreate{Name: req.Name, Driver: cmp.Or(req.Driver, "bridge"), Internal: req.Internal, Attachable: req.Attachable, EnableIPv6: req.IPv6, Labels: req.Labels}
	if req.Subnet != "" || req.Gateway != "" || req.IPRange != "" {
		for field, cidr := range map[string]string{"subnet": req.Subnet, "ip_range": req.IPRange} {
			if _, _, err := net.ParseCIDR(cidr); cidr != "" && err != nil {
				fail(w, invalid("%s must be in CIDR notation", field))
				return
			}
		}
		if req.Subnet == "" {
			fail(w, invalid("subnet is required when gateway or ip_range is set"))
			return
		}
		if req.Gateway != "" && net.ParseIP(req.Gateway) == nil {
			fail(w, invalid("gateway must be an IP address"))
			return
		}
		spec.IPAM = &engine.IPAM{Driver: "default", Config: []engine.IPAMConfig{{Subnet: req.Subnet, Gateway: req.Gateway, IPRange: req.IPRange}}}
	}
	id, err := s.Engine.CreateNetwork(r.Context(), spec)
	if err != nil {
		fail(w, err)
		return
	}
	n, err := s.Engine.InspectNetwork(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toNetworkDTO(*n))
}

func (s *Server) removeNetwork(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.RemoveNetwork(r.Context(), r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) pruneNetworks(w http.ResponseWriter, r *http.Request) {
	report, err := s.Engine.PruneNetworks(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
