package proxy

import (
	"bytes"
	"os"
)

// bodySink captures an HTTP body while it streams through a tee: whatever the
// caller reads from the attached io.Reader is also appended here. Small bodies
// stay in memory (for preview + token scanning); once they exceed memLimit the
// sink spills to a temp file so arbitrarily large bodies are still captured
// without blowing up memory. Nothing is ever truncated on the forwarding path.
type bodySink struct {
	buf        bytes.Buffer
	f          *os.File
	dir        string
	memLimit   int
	diskLimit  int64
	total      int64
	spilled    bool
	diskStopped bool
}

func newBodySink(dir string) *bodySink {
	return &bodySink{dir: dir, memLimit: memBodyLimit, diskLimit: diskBodyLimit}
}

// Write implements io.Writer. It is driven by io.TeeReader as the body is
// relayed to the client/origin.
func (s *bodySink) Write(p []byte) (int, error) {
	s.total += int64(len(p))

	// Already on disk: keep appending.
	if s.f != nil {
		return s.f.Write(p)
	}

	// Would this push us past the in-memory budget? Spill to disk if the
	// whole body is still within the on-disk ceiling.
	if s.buf.Len()+len(p) > s.memLimit {
		if s.total > s.diskLimit {
			// Larger than we are willing to capture; stop recording but
			// keep reporting the size so the user knows it existed.
			s.diskStopped = true
			return len(p), nil
		}
		if err := s.spill(); err != nil {
			return 0, err
		}
		return s.f.Write(p)
	}

	return s.buf.Write(p)
}

func (s *bodySink) spill() error {
	if s.dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, "yami-body-*.bin")
	if err != nil {
		return err
	}
	if _, err := f.Write(s.buf.Bytes()); err != nil {
		f.Close()
		return err
	}
	s.f = f
	s.spilled = true
	return nil
}

// Preview returns the in-memory copy (empty once spilled to disk).
func (s *bodySink) Preview() string {
	if s.spilled {
		return ""
	}
	return s.buf.String()
}

// File returns the on-disk capture path ("" if still in memory or stopped).
func (s *bodySink) File() string {
	if s.f != nil {
		return s.f.Name()
	}
	return ""
}

// Size returns the total number of bytes that streamed through.
func (s *bodySink) Size() int64 { return s.total }

// Stopped reports whether the body exceeded the on-disk ceiling.
func (s *bodySink) Stopped() bool { return s.diskStopped }

func (s *bodySink) Close() error {
	if s.f != nil {
		return s.f.Close()
	}
	return nil
}
