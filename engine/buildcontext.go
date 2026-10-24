package engine

import (
	"archive/tar"
	"bufio"
	"bytes"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// TarDirectory streams dir as a tar build context, honouring .dockerignore
// patterns (glob patterns with "!" exceptions, matched against slash paths
// relative to dir and their parent directories).
func TarDirectory(dir string) io.ReadCloser {
	reader, writer := io.Pipe()
	go func() {
		writer.CloseWithError(writeTar(dir, writer))
	}()
	return reader
}

func writeTar(root string, w io.Writer) error {
	ignore, err := readDockerignore(root)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(w)
	err = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if ignore.match(rel) {
			if info.IsDir() && !ignore.hasExceptions() {
				return filepath.SkipDir
			}
			return nil
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = rel
		// Normalise ownership so builds are reproducible across hosts.
		header.Uid, header.Gid, header.Uname, header.Gname = 0, 0, "", ""
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

// TarFiles builds an in-memory context from name → content pairs, e.g. a
// single Dockerfile.
func TarFiles(files map[string][]byte) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), ModTime: time.Now()}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}

type ignorePattern struct {
	pattern   string
	exception bool
}

type ignoreRules []ignorePattern

func readDockerignore(root string) (ignoreRules, error) {
	f, err := os.Open(filepath.Join(root, ".dockerignore"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rules ignoreRules
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignorePattern{}
		if strings.HasPrefix(line, "!") {
			rule.exception = true
			line = strings.TrimSpace(line[1:])
		}
		rule.pattern = strings.Trim(path.Clean("/"+line), "/")
		rules = append(rules, rule)
	}
	return rules, scanner.Err()
}

// match applies rules in order; the last matching rule wins.
func (r ignoreRules) match(rel string) bool {
	ignored := false
	for _, rule := range r {
		if matchPath(rule.pattern, rel) {
			ignored = !rule.exception
		}
	}
	return ignored
}

func (r ignoreRules) hasExceptions() bool {
	for _, rule := range r {
		if rule.exception {
			return true
		}
	}
	return false
}

// matchPath reports whether pattern matches rel or one of its parent
// directories, so "node_modules" also excludes everything beneath it.
func matchPath(pattern, rel string) bool {
	for p := rel; p != "." && p != ""; p = path.Dir(p) {
		if ok, _ := path.Match(pattern, p); ok {
			return true
		}
		if strings.HasPrefix(pattern, "**/") {
			if ok, _ := path.Match(pattern[3:], path.Base(p)); ok {
				return true
			}
		}
	}
	return false
}
