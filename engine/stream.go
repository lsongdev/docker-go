package engine

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Stream identifies the origin of a log or exec output chunk.
type Stream string

const (
	Stdout Stream = "stdout"
	Stderr Stream = "stderr"
)

// multiplexed is the content type of streams framed with 8-byte headers.
const multiplexed = "application/vnd.docker.multiplexed-stream"

// Frame is one chunk of container output.
type Frame struct {
	Stream Stream
	Data   []byte
}

// readFrames calls fn for each chunk of r. Multiplexed streams carry an
// 8-byte header per frame (stream type, 3 zero bytes, big-endian length);
// raw streams (TTY containers) are reported as stdout.
func readFrames(r io.Reader, isMultiplexed bool, fn func(Frame) error) error {
	if !isMultiplexed {
		buf := make([]byte, 32<<10)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if ferr := fn(Frame{Stream: Stdout, Data: bytes.Clone(buf[:n])}); ferr != nil {
					return ferr
				}
			}
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	var header [8]byte
	for {
		if _, err := io.ReadFull(r, header[:]); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		size := binary.BigEndian.Uint32(header[4:])
		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			return err
		}
		stream := Stdout
		switch header[0] {
		case 0, 1:
		case 2:
			stream = Stderr
		case 3: // systemerr: the daemon reports a stream failure inline
			return errors.New(strings.TrimSpace(string(data)))
		default:
			return fmt.Errorf("engine: unknown stream type %d", header[0])
		}
		if err := fn(Frame{Stream: stream, Data: data}); err != nil {
			return err
		}
	}
}

// lineSplitter turns per-stream chunks into complete lines.
type lineSplitter struct {
	pending map[Stream][]byte
	emit    func(Stream, string) error
}

func newLineSplitter(emit func(Stream, string) error) *lineSplitter {
	return &lineSplitter{pending: map[Stream][]byte{}, emit: emit}
}

func (s *lineSplitter) write(f Frame) error {
	buf := append(s.pending[f.Stream], f.Data...)
	for {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		if err := s.emit(f.Stream, strings.TrimSuffix(string(buf[:i]), "\r")); err != nil {
			return err
		}
		buf = buf[i+1:]
	}
	s.pending[f.Stream] = buf
	return nil
}

func (s *lineSplitter) flush() error {
	for _, stream := range []Stream{Stdout, Stderr} {
		if buf := s.pending[stream]; len(buf) > 0 {
			s.pending[stream] = nil
			if err := s.emit(stream, strings.TrimSuffix(string(buf), "\r")); err != nil {
				return err
			}
		}
	}
	return nil
}

// Message is one entry of the JSON progress stream returned by image pull,
// push and build.
type Message struct {
	ID       string `json:"id,omitempty"`
	Status   string `json:"status,omitempty"`
	Progress string `json:"progress,omitempty"`
	Stream   string `json:"stream,omitempty"`
	Error    string `json:"error,omitempty"`
	Detail   struct {
		Message string `json:"message,omitempty"`
	} `json:"errorDetail"`
	Aux json.RawMessage `json:"aux,omitempty"`
}

// Text renders the message as a single human-readable line.
func (m Message) Text() string {
	if m.Stream != "" {
		return strings.TrimRight(m.Stream, "\n")
	}
	parts := make([]string, 0, 3)
	if m.ID != "" {
		parts = append(parts, m.ID+":")
	}
	if m.Status != "" {
		parts = append(parts, m.Status)
	}
	if m.Progress != "" {
		parts = append(parts, m.Progress)
	}
	return strings.Join(parts, " ")
}

// readMessages decodes a JSON message stream, calling fn for each message and
// returning the first error the daemon reports.
func readMessages(r io.Reader, fn func(Message) error) error {
	decoder := json.NewDecoder(r)
	for {
		var m Message
		if err := decoder.Decode(&m); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if m.Error != "" || m.Detail.Message != "" {
			if m.Detail.Message != "" {
				return errors.New(m.Detail.Message)
			}
			return errors.New(m.Error)
		}
		if fn != nil {
			if err := fn(m); err != nil {
				return err
			}
		}
	}
}
