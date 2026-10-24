package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

var (
	ErrNotFound = errors.New("stack not found")
	ErrExists   = errors.New("stack already exists")
	ErrInvalid  = errors.New("invalid stack")
	ErrReadOnly = errors.New("stack is not managed by this panel")
)

// composeFiles are the file names compose discovers, in priority order.
var composeFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// projectName matches compose's project name rules.
var projectName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ValidateName checks that name is usable as a compose project name and as a
// directory name.
func ValidateName(name string) error {
	if !projectName.MatchString(name) || len(name) > 63 {
		return fmt.Errorf("%w: name must be lowercase letters, digits, '-' or '_' and start with a letter or digit", ErrInvalid)
	}
	return nil
}

// Store keeps compose stacks as directories under a root, one per stack:
// <root>/<name>/compose.yaml plus an optional .env. Projects started
// elsewhere are listed too, but are read-only.
type Store struct {
	root string
	cli  *CLI
	mu   sync.Mutex // serialises file writes
}

func NewStore(root string, cli *CLI) *Store { return &Store{root: root, cli: cli} }

// CLI returns the compose CLI the store runs commands with.
func (s *Store) CLI() *CLI { return s.cli }

type Status string

const (
	StatusRunning  Status = "running"  // every container running
	StatusPartial  Status = "partial"  // some containers running
	StatusStopped  Status = "stopped"  // containers exist, none running
	StatusInactive Status = "inactive" // no containers
)

// Stack is a compose project summary.
type Stack struct {
	Name        string   `json:"name"`
	Dir         string   `json:"dir"`
	ConfigFiles []string `json:"config_files"`
	Status      Status   `json:"status"`
	Containers  string   `json:"containers,omitempty"` // compose's raw summary, e.g. "running(2)"
	Managed     bool     `json:"managed"`
}

// StackDetail adds file contents and services to a stack.
type StackDetail struct {
	Stack
	Compose  string    `json:"compose"`
	Env      string    `json:"env"`
	Services []Service `json:"services"`
}

// Spec is the editable content of a managed stack.
type Spec struct {
	Compose string `json:"compose"`
	Env     string `json:"env"`
}

// parseStatus turns "running(2), exited(1)" into an overall status.
func parseStatus(summary string) Status {
	running, total := 0, 0
	for part := range strings.SplitSeq(summary, ",") {
		part = strings.TrimSpace(part)
		open, close := strings.Index(part, "("), strings.Index(part, ")")
		if open < 0 || close < open {
			continue
		}
		n, _ := strconv.Atoi(part[open+1 : close])
		total += n
		if part[:open] == "running" {
			running += n
		}
	}
	switch {
	case total == 0:
		return StatusInactive
	case running == total:
		return StatusRunning
	case running > 0:
		return StatusPartial
	default:
		return StatusStopped
	}
}

func findComposeFile(dir string) string {
	for _, name := range composeFiles {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

func (s *Store) dir(name string) string { return filepath.Join(s.root, name) }

// managed returns the managed stack named name, if its directory holds a
// compose file.
func (s *Store) managed(name string) (Stack, bool) {
	if ValidateName(name) != nil {
		return Stack{}, false
	}
	dir := s.dir(name)
	file := findComposeFile(dir)
	if file == "" {
		return Stack{}, false
	}
	return Stack{Name: name, Dir: dir, ConfigFiles: []string{file}, Status: StatusInactive, Managed: true}, true
}

// List returns managed stacks merged with every project compose knows about.
func (s *Store) List(ctx context.Context) ([]Stack, error) {
	byName := map[string]*Stack{}
	entries, err := os.ReadDir(s.root)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if stack, ok := s.managed(entry.Name()); ok {
			byName[stack.Name] = &stack
		}
	}
	projects, err := s.cli.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		stack, ok := byName[p.Name]
		if !ok {
			files := splitFiles(p.ConfigFiles)
			stack = &Stack{Name: p.Name, ConfigFiles: files}
			if len(files) > 0 {
				stack.Dir = filepath.Dir(files[0])
			}
			byName[p.Name] = stack
		}
		stack.Status = parseStatus(p.Status)
		stack.Containers = p.Status
	}
	stacks := make([]Stack, 0, len(byName))
	for _, stack := range byName {
		stacks = append(stacks, *stack)
	}
	slices.SortFunc(stacks, func(a, b Stack) int { return strings.Compare(a.Name, b.Name) })
	return stacks, nil
}

func splitFiles(list string) []string {
	var files []string
	for f := range strings.SplitSeq(list, ",") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	return files
}

// Get returns a stack with its compose file, environment and services.
func (s *Store) Get(ctx context.Context, name string) (*StackDetail, error) {
	stack, err := s.find(ctx, name)
	if err != nil {
		return nil, err
	}
	detail := &StackDetail{Stack: stack}
	if len(stack.ConfigFiles) > 0 {
		data, err := os.ReadFile(stack.ConfigFiles[0])
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		detail.Compose = string(data)
	}
	if stack.Dir != "" {
		data, err := os.ReadFile(filepath.Join(stack.Dir, ".env"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		detail.Env = string(data)
	}
	if detail.Services, err = s.cli.PS(ctx, s.project(stack)); err != nil {
		return nil, err
	}
	return detail, nil
}

// find resolves a stack by name, preferring the managed copy.
func (s *Store) find(ctx context.Context, name string) (Stack, error) {
	stacks, err := s.List(ctx)
	if err != nil {
		return Stack{}, err
	}
	for _, stack := range stacks {
		if stack.Name == name {
			return stack, nil
		}
	}
	return Stack{}, fmt.Errorf("%w: %s", ErrNotFound, name)
}

func (s *Store) project(stack Stack) Project {
	return Project{Name: stack.Name, Dir: stack.Dir, Files: stack.ConfigFiles}
}

// Create writes a new managed stack after validating it with compose. It
// does not start it.
func (s *Store) Create(ctx context.Context, name string, spec Spec) (*Stack, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := s.dir(name)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrExists, name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := s.write(ctx, name, dir, filepath.Join(dir, composeFiles[0]), spec); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	stack, _ := s.managed(name)
	return &stack, nil
}

// Update replaces a managed stack's files, keeping the previous content if
// the new configuration is invalid. Running services are not recreated.
func (s *Store) Update(ctx context.Context, name string, spec Spec) error {
	stack, ok := s.managed(name)
	if !ok {
		if _, err := s.find(ctx, name); err != nil {
			return err
		}
		return fmt.Errorf("%w: %s", ErrReadOnly, name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, envFile := stack.ConfigFiles[0], filepath.Join(stack.Dir, ".env")
	oldCompose, _ := os.ReadFile(file)
	oldEnv, envErr := os.ReadFile(envFile)
	if err := s.write(ctx, name, stack.Dir, file, spec); err != nil {
		os.WriteFile(file, oldCompose, 0o644)
		if envErr == nil {
			os.WriteFile(envFile, oldEnv, 0o600)
		} else {
			os.Remove(envFile)
		}
		return err
	}
	return nil
}

// write stores spec and validates the result with `compose config`.
func (s *Store) write(ctx context.Context, name, dir, file string, spec Spec) error {
	if strings.TrimSpace(spec.Compose) == "" {
		return fmt.Errorf("%w: compose file is empty", ErrInvalid)
	}
	if err := os.WriteFile(file, []byte(spec.Compose), 0o644); err != nil {
		return err
	}
	envFile := filepath.Join(dir, ".env")
	if strings.TrimSpace(spec.Env) == "" {
		if err := os.Remove(envFile); err != nil && !os.IsNotExist(err) {
			return err
		}
	} else if err := os.WriteFile(envFile, []byte(spec.Env), 0o600); err != nil {
		return err
	}
	if _, err := s.cli.Config(ctx, Project{Name: name, Dir: dir, Files: []string{file}}); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return nil
}

// Action is a lifecycle operation on a stack.
type Action string

const (
	ActionUp      Action = "up"
	ActionDown    Action = "down"
	ActionStart   Action = "start"
	ActionStop    Action = "stop"
	ActionRestart Action = "restart"
	ActionPull    Action = "pull"
)

// ParseAction validates an action name.
func ParseAction(name string) (Action, error) {
	switch a := Action(name); a {
	case ActionUp, ActionDown, ActionStart, ActionStop, ActionRestart, ActionPull:
		return a, nil
	}
	return "", fmt.Errorf("%w: unsupported action %q", ErrInvalid, name)
}

// Do runs a lifecycle action, streaming compose output to out.
func (s *Store) Do(ctx context.Context, name string, action Action, out func(string)) error {
	stack, err := s.find(ctx, name)
	if err != nil {
		return err
	}
	p := s.project(stack)
	switch action {
	case ActionUp:
		return s.cli.Up(ctx, p, UpOptions{}, out)
	case ActionDown:
		return s.cli.Down(ctx, p, false, out)
	default:
		return s.cli.Run(ctx, p, out, string(action))
	}
}

// Delete takes a managed stack down and removes its directory.
func (s *Store) Delete(ctx context.Context, name string, removeVolumes bool, out func(string)) error {
	stack, ok := s.managed(name)
	if !ok {
		if _, err := s.find(ctx, name); err != nil {
			return err
		}
		return fmt.Errorf("%w: %s", ErrReadOnly, name)
	}
	if err := s.cli.Down(ctx, s.project(stack), removeVolumes, out); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(stack.Dir)
}
