package dreams

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type toolArgs struct {
	Target         string `json:"target"`
	SessionID      string `json:"session_id"`
	Path           string `json:"path"`
	Offset         int    `json:"offset"`
	Limit          int    `json:"limit"`
	Content        string `json:"content"`
	ExpectedSHA256 string `json:"expected_sha256"`
	OldText        string `json:"old_text"`
	NewText        string `json:"new_text"`
	Pattern        string `json:"pattern"`
	Query          string `json:"query"`
}

type span struct{ start, end int }

type readTracker struct {
	length int
	ranges []span
}

func (r *readTracker) add(start, end int) {
	r.ranges = append(r.ranges, span{start, end})
	sort.Slice(r.ranges, func(i, j int) bool { return r.ranges[i].start < r.ranges[j].start })
	merged := make([]span, 0, len(r.ranges))
	for _, next := range r.ranges {
		if len(merged) == 0 || next.start > merged[len(merged)-1].end {
			merged = append(merged, next)
			continue
		}
		if next.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = next.end
		}
	}
	r.ranges = merged
}

func (r *readTracker) complete() bool {
	return r != nil && len(r.ranges) == 1 && r.ranges[0].start == 0 && r.ranges[0].end == r.length
}

type toolkit struct {
	backend     Backend
	storeID     string
	transcripts map[string]Transcript
	sources     map[string]*readTracker
	memoryReads map[string]*readTracker
	versions    map[string]string
	limits      Limits
	organizer   bool
	unverified  map[string]string
	writeTurns  map[string]int
	turn        int
}

func newToolkit(backend Backend, storeID string, transcripts map[string]Transcript, limits Limits) *toolkit {
	return &toolkit{backend: backend, storeID: storeID, transcripts: transcripts, limits: limits, sources: map[string]*readTracker{}, memoryReads: map[string]*readTracker{}, versions: map[string]string{}, unverified: map[string]string{}, writeTurns: map[string]int{}}
}

func (t *toolkit) sourceRead(id string) bool { return t.sources[id].complete() }

func (t *toolkit) execute(ctx context.Context, name string, raw json.RawMessage) (string, Event, error) {
	if err := validateArguments(name, raw); err != nil {
		return "", Event{}, err
	}
	var args toolArgs
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return "", Event{}, errors.New("invalid tool arguments")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return "", Event{}, errors.New("invalid tool arguments")
	}
	if err := ctx.Err(); err != nil {
		return "", Event{}, err
	}
	switch name {
	case "read":
		return t.read(ctx, args)
	case "write":
		return t.write(ctx, args)
	case "edit":
		return t.edit(ctx, args)
	case "glob":
		return t.glob(ctx, args)
	case "grep":
		return t.grep(ctx, args)
	default:
		return "", Event{}, errors.New("tool is not permitted; only read, write, edit, glob and grep are available")
	}
}

type readResult struct {
	Content         string `json:"content"`
	SHA256          string `json:"sha256"`
	Offset          int    `json:"offset"`
	NextOffset      int    `json:"next_offset"`
	TotalCharacters int    `json:"total_characters"`
	EOF             bool   `json:"eof"`
}

func (t *toolkit) read(ctx context.Context, a toolArgs) (string, Event, error) {
	var text, hash string
	var tracker *readTracker
	switch a.Target {
	case "source":
		source, ok := t.transcripts[a.SessionID]
		if !ok {
			return "", Event{}, errors.New("source session is outside this stage's input set")
		}
		text, hash = source.Text, digest(source.Text)
		tracker = t.sources[a.SessionID]
		if tracker == nil {
			tracker = &readTracker{length: utf8.RuneCountInString(text)}
			t.sources[a.SessionID] = tracker
		}
	case "memory":
		if err := validPath(a.Path); err != nil {
			return "", Event{}, err
		}
		memory, err := t.backend.Read(ctx, t.storeID, a.Path)
		if err != nil {
			return "", Event{}, errors.New("memory is unavailable; use glob to discover current paths")
		}
		text, hash = memory.Content, memory.SHA256
		if hash == "" || digest(text) != hash {
			return "", Event{}, errors.New("memory content hash mismatch")
		}
		key := a.Path + ":" + hash
		tracker = t.memoryReads[key]
		if tracker == nil {
			tracker = &readTracker{length: utf8.RuneCountInString(text)}
			t.memoryReads[key] = tracker
		}
	default:
		return "", Event{}, errors.New("read target must be source or memory")
	}
	if !utf8.ValidString(text) {
		return "", Event{}, errors.New("input is not valid UTF-8")
	}
	chars := []rune(text)
	start, end, err := window(len(chars), a.Offset, a.Limit, 12000, 32768)
	if err != nil {
		return "", Event{}, err
	}
	tracker.add(start, end)
	if a.Target == "memory" && tracker.complete() {
		t.versions[a.Path] = hash
		if t.unverified[a.Path] == hash && t.turn > t.writeTurns[a.Path] {
			delete(t.unverified, a.Path)
			delete(t.writeTurns, a.Path)
		}
	}
	result := readResult{Content: string(chars[start:end]), SHA256: hash, Offset: start, NextOffset: end, TotalCharacters: len(chars), EOF: end == len(chars)}
	return encodeResult(result), Event{Path: a.Path, Bytes: len(result.Content), SHA256: hash}, nil
}

func (t *toolkit) write(ctx context.Context, a toolArgs) (string, Event, error) {
	if err := validPath(a.Path); err != nil {
		return "", Event{}, err
	}
	if !utf8.ValidString(a.Content) || len(a.Content) > t.limits.MaxMemoryBytes {
		return "", Event{}, errors.New("memory must be valid UTF-8 within the document size limit")
	}
	if a.ExpectedSHA256 != "" && t.versions[a.Path] != a.ExpectedSHA256 {
		return "", Event{}, errors.New("read the complete current memory before overwriting it")
	}
	if a.ExpectedSHA256 == "" {
		items, err := t.backend.List(ctx, t.storeID)
		if err != nil {
			return "", Event{}, errors.New("memory catalog is unavailable")
		}
		for _, item := range items {
			if item.Path == a.Path {
				return "", Event{}, errors.New("path already exists; read it and supply expected_sha256")
			}
		}
		if len(items) >= t.limits.MaxMemories {
			return "", Event{}, errors.New("memory store document limit reached")
		}
	}
	if err := t.validateRetirement(ctx, a); err != nil {
		return "", Event{}, err
	}
	result, err := t.backend.Write(ctx, t.storeID, Memory{Path: a.Path, Content: a.Content}, a.ExpectedSHA256)
	if err != nil {
		return "", Event{}, errors.New("memory write failed or version changed; reread before retrying")
	}
	if result.SHA256 != digest(a.Content) {
		return "", Event{}, errors.New("persisted memory hash does not match the write")
	}
	delete(t.versions, a.Path)
	delete(t.memoryReads, a.Path+":"+result.SHA256)
	t.unverified[a.Path] = result.SHA256
	t.writeTurns[a.Path] = t.turn
	return encodeResult(struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}{result.Path, result.SHA256}), Event{Path: a.Path, Bytes: len(a.Content), SHA256: result.SHA256}, nil
}

func (t *toolkit) edit(ctx context.Context, a toolArgs) (string, Event, error) {
	if err := validPath(a.Path); err != nil {
		return "", Event{}, err
	}
	if a.OldText == "" || a.ExpectedSHA256 == "" {
		return "", Event{}, errors.New("edit requires nonempty old_text and expected_sha256")
	}
	if t.versions[a.Path] != a.ExpectedSHA256 {
		return "", Event{}, errors.New("read the complete current memory before editing it")
	}
	current, err := t.backend.Read(ctx, t.storeID, a.Path)
	if err != nil || current.SHA256 != a.ExpectedSHA256 {
		return "", Event{}, errors.New("memory version changed; reread before editing")
	}
	if strings.Count(current.Content, a.OldText) != 1 {
		return "", Event{}, errors.New("old_text must match exactly once")
	}
	a.Content = strings.Replace(current.Content, a.OldText, a.NewText, 1)
	return t.write(ctx, a)
}

type catalogItem struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type catalogResult struct {
	Entries    []catalogItem `json:"entries"`
	NextOffset int           `json:"next_offset"`
	Total      int           `json:"total"`
}

func (t *toolkit) glob(ctx context.Context, a toolArgs) (string, Event, error) {
	pattern := a.Pattern
	if pattern == "" {
		pattern = "*"
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return "", Event{}, errors.New("invalid glob pattern")
	}
	memories, err := t.backend.List(ctx, t.storeID)
	if err != nil {
		return "", Event{}, errors.New("memory catalog is unavailable")
	}
	items := make([]catalogItem, 0)
	for _, item := range memories {
		match, _ := path.Match(pattern, item.Path)
		if pattern == "*" || match {
			items = append(items, catalogItem{item.Path, item.SHA256})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	start, end, err := window(len(items), a.Offset, a.Limit, 100, 100)
	if err != nil {
		return "", Event{}, err
	}
	return encodeResult(catalogResult{items[start:end], end, len(items)}), Event{}, nil
}

type matchResult struct {
	Path    string `json:"path"`
	Excerpt string `json:"excerpt"`
}

func (t *toolkit) grep(ctx context.Context, a toolArgs) (string, Event, error) {
	if a.Query == "" || len(a.Query) > 4096 {
		return "", Event{}, errors.New("grep requires a literal query of 1 to 4096 bytes")
	}
	memories, err := t.backend.List(ctx, t.storeID)
	if err != nil {
		return "", Event{}, errors.New("memory catalog is unavailable")
	}
	sort.Slice(memories, func(i, j int) bool { return memories[i].Path < memories[j].Path })
	matches := make([]matchResult, 0)
	for _, item := range memories {
		memory, err := readCatalogMemory(ctx, t.backend, t.storeID, item)
		if err != nil {
			return "", Event{}, errors.New("memory search failed; not all pages could be read")
		}
		if index := strings.Index(memory.Content, a.Query); index >= 0 {
			excerpt := []rune(memory.Content[index:])
			if len(excerpt) > 256 {
				excerpt = excerpt[:256]
			}
			matches = append(matches, matchResult{item.Path, string(excerpt)})
		}
	}
	start, end, err := window(len(matches), a.Offset, a.Limit, 50, 100)
	if err != nil {
		return "", Event{}, err
	}
	return encodeResult(struct {
		Matches    []matchResult `json:"matches"`
		NextOffset int           `json:"next_offset"`
		Total      int           `json:"total"`
	}{matches[start:end], end, len(matches)}), Event{}, nil
}

func window(length, offset, limit, fallback, maximum int) (int, int, error) {
	if limit == 0 {
		limit = fallback
	}
	if offset < 0 || offset > length || limit < 1 || limit > maximum {
		return 0, 0, errors.New("invalid offset or limit")
	}
	end := length
	if limit < length-offset {
		end = offset + limit
	}
	return offset, end, nil
}

func validPath(p string) error {
	if !utf8.ValidString(p) || len(p) < 2 || len(p) > 1024 || p[0] != '/' || strings.Contains(p, "\\") || path.Clean(p) != p || strings.HasSuffix(p, "/") {
		return errors.New("memory path must be a canonical absolute path within the store")
	}
	for _, r := range p {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return errors.New("memory path contains a forbidden character")
		}
	}
	return nil
}

func digest(s string) string { hash := sha256.Sum256([]byte(s)); return hex.EncodeToString(hash[:]) }

func encodeResult(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("encoding failure: %T", value)
	}
	return string(encoded)
}

func validateArguments(name string, raw json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return errors.New("tool arguments must be an object")
	}
	required := map[string][]string{"read": {"target"}, "write": {"path", "content"}, "edit": {"path", "old_text", "new_text", "expected_sha256"}, "glob": {}, "grep": {"query"}}
	keys, ok := required[name]
	if !ok {
		return errors.New("tool is not permitted")
	}
	for _, key := range keys {
		value, exists := fields[key]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("missing required tool argument: %s", key)
		}
	}
	return nil
}
