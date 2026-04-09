// Package clauderecord captures, masks, and replays Claude CLI NDJSON sessions
// for integration testing and fixture generation.
package clauderecord

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Direction indicates whether a frame was sent to or received from the CLI.
type Direction string

const (
	DirSend Direction = "send" // stdin → claude
	DirRecv Direction = "recv" // claude → stdout
)

// Entry is one NDJSON line in a recording.
type Entry struct {
	Timestamp time.Time       `json:"ts"`
	Direction Direction       `json:"dir"`
	Seq       int             `json:"seq"`
	Frame     json.RawMessage `json:"frame"`
}

// Recorder captures NDJSON frames exchanged with the Claude CLI.
type Recorder struct {
	mu      sync.Mutex
	entries []Entry
	seq     int
	start   time.Time
}

// NewRecorder creates a new recording session.
func NewRecorder() *Recorder {
	return &Recorder{start: time.Now().UTC()}
}

// RecordSend records a frame sent to the CLI's stdin.
func (r *Recorder) RecordSend(frame []byte) {
	r.record(DirSend, frame)
}

// RecordRecv records a frame received from the CLI's stdout.
func (r *Recorder) RecordRecv(frame []byte) {
	r.record(DirRecv, frame)
}

func (r *Recorder) record(dir Direction, frame []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	trimmed := trimBytes(frame)
	if len(trimmed) == 0 {
		return
	}
	r.entries = append(r.entries, Entry{
		Timestamp: time.Now().UTC(),
		Direction: dir,
		Seq:       r.seq,
		Frame:     json.RawMessage(trimmed),
	})
	r.seq++
}

// Entries returns a copy of all recorded entries.
func (r *Recorder) Entries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, len(r.entries))
	copy(out, r.entries)
	return out
}

// SaveRaw writes the unmasked recording to a file.
func (r *Recorder) SaveRaw(path string) error {
	return saveEntries(path, r.Entries())
}

// SaveMasked applies privacy masking and writes the result to a file.
func (r *Recorder) SaveMasked(path string, opts MaskOptions) error {
	masked := MaskEntries(r.Entries(), opts)
	return saveEntries(path, masked)
}

func saveEntries(path string, entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// LoadFixture reads a recording from a NDJSON file.
func LoadFixture(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20) // 1MB line buffer
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, scanner.Err()
}

// RecvEntries returns only the entries received from the CLI.
func RecvEntries(entries []Entry) []Entry {
	var out []Entry
	for _, e := range entries {
		if e.Direction == DirRecv {
			out = append(out, e)
		}
	}
	return out
}

// SendEntries returns only the entries sent to the CLI.
func SendEntries(entries []Entry) []Entry {
	var out []Entry
	for _, e := range entries {
		if e.Direction == DirSend {
			out = append(out, e)
		}
	}
	return out
}

func trimBytes(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}
