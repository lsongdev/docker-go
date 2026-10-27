// Package compose drives Docker Compose projects through the docker compose
// CLI and manages compose stacks stored on disk.
//
// Compose's model (interpolation, profiles, dependency ordering, build,
// healthchecks) is large and evolves quickly, so this package delegates to
// the official CLI rather than reimplementing it.
package compose

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// CLI runs docker compose commands. The zero CLI locates compose on first
// use and keeps looking while none is installed, so its commands fail until
// Docker is installed and then work without a restart.
type CLI struct {
	mu      sync.Mutex
	command []string // e.g. ["docker", "compose"] or ["docker-compose"]
}

// NewCLI locates the compose plugin (`docker compose`) or the standalone
// `docker-compose` binary.
func NewCLI() (*CLI, error) {
	command := locate()
	if command == nil {
		return nil, errors.New("compose: neither `docker compose` nor `docker-compose` is available")
	}
	return &CLI{command: command}, nil
}

func locate() []string {
	if docker, err := exec.LookPath("docker"); err == nil {
		if exec.Command(docker, "compose", "version").Run() == nil {
			return []string{docker, "compose"}
		}
	}
	if standalone, err := exec.LookPath("docker-compose"); err == nil {
		return []string{standalone}
	}
	return nil
}

// find returns the compose command, or docker-compose (which then fails as
// not found) while none is installed.
func (c *CLI) find() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.command == nil {
		c.command = locate()
	}
	if c.command == nil {
		return []string{"docker-compose"}
	}
	return c.command
}

// Version returns the compose version string.
func (c *CLI) Version(ctx context.Context) (string, error) {
	out, err := c.output(ctx, nil, "version", "--short")
	return strings.TrimSpace(string(out)), err
}

// Project identifies a compose project on disk.
type Project struct {
	Name  string
	Dir   string   // project directory: relative paths and .env resolve here
	Files []string // compose files; empty lets compose discover them in Dir
	Env   []string // extra KEY=VALUE variables, taking precedence over .env
}

func (p Project) args() []string {
	args := []string{"--ansi", "never", "--progress", "plain"}
	if p.Name != "" {
		args = append(args, "--project-name", p.Name)
	}
	if p.Dir != "" {
		args = append(args, "--project-directory", p.Dir)
	}
	for _, f := range p.Files {
		args = append(args, "--file", f)
	}
	return args
}

func (c *CLI) cmd(ctx context.Context, p *Project, args ...string) *exec.Cmd {
	command := c.find()
	all := append([]string{}, command[1:]...)
	if p != nil {
		all = append(all, p.args()...)
	}
	all = append(all, args...)
	cmd := exec.CommandContext(ctx, command[0], all...)
	configureCancel(cmd)
	cmd.Env = os.Environ()
	if p != nil {
		cmd.Dir = p.Dir
		cmd.Env = append(cmd.Env, p.Env...)
	}
	return cmd
}

// output runs a command and returns stdout; stderr becomes the error text.
func (c *CLI) output(ctx context.Context, p *Project, args ...string) ([]byte, error) {
	cmd := c.cmd(ctx, p, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, commandError(ctx, args, stderr.String(), err)
	}
	return out, nil
}

func commandError(ctx context.Context, args []string, stderr string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if msg := strings.TrimSpace(stderr); msg != "" {
		return fmt.Errorf("compose %s: %s", args[0], msg)
	}
	return fmt.Errorf("compose %s: %w", args[0], err)
}

// Run executes a compose subcommand, streaming each output line (stdout and
// stderr interleaved) to out.
func (c *CLI) Run(ctx context.Context, p Project, out func(string), args ...string) error {
	cmd := c.cmd(ctx, &p, args...)
	reader, writer := io.Pipe()
	cmd.Stdout, cmd.Stderr = writer, writer
	var tail lastLines
	var wg sync.WaitGroup
	wg.Go(func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64<<10), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			tail.add(line)
			if out != nil {
				out(line)
			}
		}
		if err := scanner.Err(); err != nil && out != nil {
			out("(output truncated: " + err.Error() + ")")
		}
		// Keep draining so compose never blocks on a full pipe.
		io.Copy(io.Discard, reader)
	})
	err := cmd.Run()
	writer.Close()
	wg.Wait()
	if err != nil {
		return commandError(ctx, args, tail.String(), err)
	}
	return nil
}

// lastLines keeps the final lines of output for error messages.
type lastLines struct{ lines []string }

func (l *lastLines) add(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	l.lines = append(l.lines, line)
	if len(l.lines) > 5 {
		l.lines = l.lines[1:]
	}
}

func (l *lastLines) String() string { return strings.Join(l.lines, "\n") }

// Up creates and starts the project's services in the background.
func (c *CLI) Up(ctx context.Context, p Project, opts UpOptions, out func(string)) error {
	args := []string{"up", "--detach", "--remove-orphans"}
	if opts.Pull != "" {
		args = append(args, "--pull", opts.Pull)
	}
	if opts.Build {
		args = append(args, "--build")
	}
	if opts.ForceRecreate {
		args = append(args, "--force-recreate")
	}
	if opts.Wait {
		args = append(args, "--wait")
	}
	return c.Run(ctx, p, out, args...)
}

type UpOptions struct {
	Pull          string // always, missing, never; empty uses compose's default
	Build         bool
	ForceRecreate bool
	Wait          bool // wait for services to be running/healthy
}

// Down stops and removes the project's containers and networks.
func (c *CLI) Down(ctx context.Context, p Project, removeVolumes bool, out func(string)) error {
	args := []string{"down", "--remove-orphans"}
	if removeVolumes {
		args = append(args, "--volumes")
	}
	return c.Run(ctx, p, out, args...)
}

// Config validates the project and returns the fully resolved configuration.
func (c *CLI) Config(ctx context.Context, p Project) (string, error) {
	out, err := c.output(ctx, &p, "config")
	return string(out), err
}

// ProjectSummary is an entry of `docker compose ls`.
type ProjectSummary struct {
	Name        string `json:"Name"`
	Status      string `json:"Status"`      // e.g. "running(2), exited(1)"
	ConfigFiles string `json:"ConfigFiles"` // comma separated
}

// List returns every compose project known to the daemon, including stopped
// ones.
func (c *CLI) List(ctx context.Context) ([]ProjectSummary, error) {
	out, err := c.output(ctx, nil, "ls", "--all", "--format", "json")
	if err != nil {
		return nil, err
	}
	var projects []ProjectSummary
	if err := json.Unmarshal(bytes.TrimSpace(out), &projects); err != nil && len(bytes.TrimSpace(out)) > 0 {
		return nil, fmt.Errorf("compose ls: %w", err)
	}
	return projects, nil
}

// Service is a container of a compose project, from `docker compose ps`.
type Service struct {
	ID         string      `json:"ID"`
	Name       string      `json:"Name"`
	Service    string      `json:"Service"`
	Image      string      `json:"Image"`
	State      string      `json:"State"`
	Status     string      `json:"Status"`
	Health     string      `json:"Health"`
	ExitCode   int         `json:"ExitCode"`
	Publishers []Publisher `json:"Publishers"`
}

type Publisher struct {
	URL           string `json:"URL"`
	TargetPort    int    `json:"TargetPort"`
	PublishedPort int    `json:"PublishedPort"`
	Protocol      string `json:"Protocol"`
}

// PS lists the project's containers, including stopped ones.
func (c *CLI) PS(ctx context.Context, p Project) ([]Service, error) {
	out, err := c.output(ctx, &p, "ps", "--all", "--format", "json")
	if err != nil {
		return nil, err
	}
	return parseServices(out)
}

// parseServices accepts both output styles of `ps --format json`: a JSON
// array (compose < 2.21) and one object per line (newer releases).
func parseServices(out []byte) ([]Service, error) {
	out = bytes.TrimSpace(out)
	services := []Service{}
	if len(out) == 0 {
		return services, nil
	}
	if out[0] == '[' {
		err := json.Unmarshal(out, &services)
		return services, err
	}
	for line := range bytes.SplitSeq(out, []byte("\n")) {
		if line = bytes.TrimSpace(line); len(line) == 0 {
			continue
		}
		var s Service
		if err := json.Unmarshal(line, &s); err != nil {
			return nil, fmt.Errorf("compose ps: %w", err)
		}
		services = append(services, s)
	}
	return services, nil
}
