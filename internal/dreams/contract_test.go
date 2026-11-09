package dreams

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func requestFixture() Request {
	return Request{Inputs: []Input{{Type: "memory_store", MemoryStoreID: "memstore_source"}, {Type: "sessions", SessionIDs: []string{"sesn_b", "sesn_a"}}}, Model: Model{ID: "claude-test", Speed: "standard"}, OutputBehavior: OutputBehavior{Type: "create_new"}}
}

func TestDecodeRequestNormalizesAndCopies(t *testing.T) {
	input := `{"model":"claude-test","inputs":[{"type":"sessions","session_ids":["sesn_b","sesn_a"]},{"type":"memory_store","memory_store_id":"memstore_source"}]}`
	req, err := DecodeRequest(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model.Speed != "standard" || req.OutputBehavior.Type != "create_new" || req.Inputs[1].SessionIDs[0] != "sesn_a" {
		t.Fatalf("unexpected normalized request: %+v", req)
	}
	copy, err := Normalize(req)
	if err != nil {
		t.Fatal(err)
	}
	copy.Inputs[1].SessionIDs[0] = "changed"
	if req.Inputs[1].SessionIDs[0] != "sesn_a" {
		t.Fatal("normalization aliases caller storage")
	}
}

func TestRequestValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{"missing input", func(r *Request) { r.Inputs = r.Inputs[:1] }},
		{"duplicate stores", func(r *Request) { r.Inputs[1] = r.Inputs[0] }},
		{"duplicate sessions", func(r *Request) { r.Inputs[1].SessionIDs = []string{"s", "s"} }},
		{"zero sessions", func(r *Request) { r.Inputs[1].SessionIDs = nil }},
		{"too many sessions", func(r *Request) { r.Inputs[1].SessionIDs = make([]string, 101) }},
		{"foreign field", func(r *Request) { r.Inputs[0].SessionIDs = []string{} }},
		{"unknown input", func(r *Request) { r.Inputs[0].Type = "files" }},
		{"empty model", func(r *Request) { r.Model.ID = "" }},
		{"long model", func(r *Request) { r.Model.ID = strings.Repeat("中", 257) }},
		{"fast model", func(r *Request) { r.Model.Speed = "fast" }},
		{"empty instructions", func(r *Request) { s := ""; r.Instructions = &s }},
		{"long instructions", func(r *Request) { s := strings.Repeat("中", 4097); r.Instructions = &s }},
		{"wrong output store", func(r *Request) { r.OutputBehavior = OutputBehavior{Type: "update_existing", MemoryStoreID: "other"} }},
		{"missing output store", func(r *Request) { r.OutputBehavior = OutputBehavior{Type: "update_existing"} }},
		{"create target", func(r *Request) { r.OutputBehavior.MemoryStoreID = "other" }},
		{"unknown mode", func(r *Request) { r.OutputBehavior.Type = "overwrite" }},
		{"path ID", func(r *Request) { r.Inputs[1].SessionIDs[0] = "../secrets" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := requestFixture()
			test.mutate(&r)
			if _, err := Normalize(r); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("wanted invalid request, got %v", err)
			}
		})
	}
}

func TestDecodeRejectsMalformedJSONAndUnknownFields(t *testing.T) {
	base := `{"model":"claude-test","inputs":[{"type":"memory_store","memory_store_id":"m"},{"type":"sessions","session_ids":["s"]}]}`
	for _, raw := range []string{`null`, `{}`, base + `{}`, strings.Replace(base, `"claude-test"`, `null`, 1), strings.Replace(base, `"claude-test"`, `{"id":"m","speed":"fast"}`, 1), strings.Replace(base, `"model":`, `"extra":true,"model":`, 1), strings.Replace(base, `"model":`, `"output_behavior":{},"model":`, 1)} {
		t.Run(raw, func(t *testing.T) {
			if _, err := DecodeRequest(strings.NewReader(raw)); err == nil {
				t.Fatal("accepted invalid request")
			}
		})
	}
}

func TestUnicodeInstructionLimitAndUpdateContract(t *testing.T) {
	r := requestFixture()
	s := strings.Repeat("中", 4096)
	r.Instructions = &s
	r.OutputBehavior = OutputBehavior{Type: "update_existing", MemoryStoreID: "memstore_source"}
	if _, err := Normalize(r); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r)
	if _, err := DecodeRequest(strings.NewReader(string(raw))); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleAndPartialOutput(t *testing.T) {
	now := time.Date(2026, 11, 9, 0, 0, 0, 123, time.UTC)
	d, err := New("drm_test", requestFixture(), now)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outputs == nil || d.Status != Pending || d.SessionID != nil {
		t.Fatal("invalid initial resource")
	}
	if err := d.Archive(now); !errors.Is(err, ErrState) {
		t.Fatal("archived pending")
	}
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	d.Outputs = []Output{{Type: "memory_store", MemoryStoreID: "partial"}}
	if err := d.Cancel(now); err != nil {
		t.Fatal(err)
	}
	if err := d.Cancel(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := d.Finish(Completed, nil, now); err != nil {
		t.Fatal(err)
	}
	if d.Status != Canceled || len(d.Outputs) != 1 || !d.EndedAt.Equal(now) {
		t.Fatal("cancel was overwritten or partial output lost")
	}
	if err := d.Archive(now); err != nil {
		t.Fatal(err)
	}
	if err := d.Archive(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if !d.ArchivedAt.Equal(now) || d.Status != Canceled {
		t.Fatal("archive not idempotent")
	}
}

func TestCompletedAndFailedCannotCancel(t *testing.T) {
	for _, status := range []Status{Completed, Failed} {
		t.Run(string(status), func(t *testing.T) {
			d, _ := New("drm_test", requestFixture(), time.Now())
			_ = d.Start()
			var failure *Failure
			if status == Failed {
				failure = &Failure{Type: "internal_error", Message: "failed"}
			}
			if err := d.Finish(status, failure, time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := d.Cancel(time.Now()); !errors.Is(err, ErrState) {
				t.Fatal("canceled terminal job")
			}
			if err := d.Start(); !errors.Is(err, ErrState) {
				t.Fatal("restarted terminal job")
			}
		})
	}
}
