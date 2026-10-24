package engine

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FromEnv builds a client the same way the docker CLI selects a daemon:
// DOCKER_HOST, then DOCKER_CONTEXT, then the current context in the docker
// config directory, then the default local socket. DOCKER_TLS_VERIFY,
// DOCKER_CERT_PATH and DOCKER_API_VERSION are honoured.
func FromEnv() (*Client, error) {
	cfg := Config{Host: os.Getenv("DOCKER_HOST"), APIVersion: os.Getenv("DOCKER_API_VERSION")}
	if cfg.Host == "" {
		host, err := contextHost()
		if err != nil {
			return nil, err
		}
		cfg.Host = host
	}
	if os.Getenv("DOCKER_TLS_VERIFY") != "" || os.Getenv("DOCKER_CERT_PATH") != "" {
		tlsConfig, err := tlsFromDir(os.Getenv("DOCKER_CERT_PATH"), os.Getenv("DOCKER_TLS_VERIFY") != "")
		if err != nil {
			return nil, err
		}
		cfg.TLS = tlsConfig
	}
	return New(cfg)
}

func dockerConfigDir() string {
	if dir := os.Getenv("DOCKER_CONFIG"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".docker")
}

// contextHost resolves the daemon address of the active docker context, or
// returns an empty string for the default context.
func contextHost() (string, error) {
	name := os.Getenv("DOCKER_CONTEXT")
	dir := dockerConfigDir()
	if name == "" {
		data, err := os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil {
			return "", nil
		}
		var config struct {
			CurrentContext string `json:"currentContext"`
		}
		if json.Unmarshal(data, &config) != nil {
			return "", nil
		}
		name = config.CurrentContext
	}
	if name == "" || name == "default" {
		return "", nil
	}
	sum := sha256.Sum256([]byte(name))
	data, err := os.ReadFile(filepath.Join(dir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json"))
	if err != nil {
		return "", fmt.Errorf("engine: docker context %q: %w", name, err)
	}
	var meta struct {
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("engine: docker context %q: %w", name, err)
	}
	host := meta.Endpoints["docker"].Host
	if host == "" {
		return "", fmt.Errorf("engine: docker context %q has no docker endpoint", name)
	}
	return host, nil
}

func tlsFromDir(dir string, verify bool) (*tls.Config, error) {
	if dir == "" {
		dir = dockerConfigDir()
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: !verify}
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem"))
	if err == nil {
		cfg.Certificates = []tls.Certificate{cert}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("engine: load client certificate: %w", err)
	}
	if ca, err := os.ReadFile(filepath.Join(dir, "ca.pem")); err == nil {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, errors.New("engine: invalid ca.pem")
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}
