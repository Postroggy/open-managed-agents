package main

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/dreams"
)

func TestRunRecordsArePrivateAndNeverReuseDirectories(t *testing.T) {
	target := filepath.Join(t.TempDir(), "run")
	dir, err := prepareDirectory(target)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatal("records are not private")
	}
	if _, err := prepareDirectory(target); err == nil {
		t.Fatal("reused an existing record directory")
	}
}

func TestResultCheckpointsPreservePartialOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	d := dreams.Dream{ID: "drm_test", Status: dreams.Running, CreatedAt: time.Now(), Outputs: []dreams.Output{{Type: "memory_store", MemoryStoreID: "memstore_partial"}}}
	if err := writeResult(path, &d); err != nil {
		t.Fatal(err)
	}
	if err := d.Cancel(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := writeResult(path, &d); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got dreams.Dream
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != dreams.Canceled || len(got.Outputs) != 1 {
		t.Fatal("checkpoint lost terminal state or partial output")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("result permissions are not private")
	}
}

func TestCLIValidationIsSideEffectFree(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "request.json")
	body := `{"model":"provider/model","inputs":[{"type":"memory_store","memory_store_id":"memstore_input"},{"type":"sessions","session_ids":["sesn_input"]}]}`
	if err := os.WriteFile(input, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	target := filepath.Join(dir, "must-not-exist")
	if err := run(logger, []string{"-request", input, "-records-dir", target}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("validation created a run directory")
	}
	if err := run(logger, []string{"-request", input, "-max-turns", "-1"}); err == nil {
		t.Fatal("accepted invalid limits")
	}
	if err := run(logger, []string{}); err == nil {
		t.Fatal("accepted missing request")
	}
	if err := run(logger, []string{"-request", input, "unexpected"}); err == nil {
		t.Fatal("accepted extra arguments")
	}
}

func TestCLIRejectsUpdateBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "request.json")
	body := `{"model":"provider/model","inputs":[{"type":"memory_store","memory_store_id":"memstore_input"},{"type":"sessions","session_ids":["sesn_input"]}],"output_behavior":{"type":"update_existing","memory_store_id":"memstore_input"}}`
	if err := os.WriteFile(input, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run(logger, []string{"-request", input, "-execute"}); !errors.Is(err, dreams.ErrUpdateRequiresCoordinator) {
		t.Fatalf("unsupported update reached execution: %v", err)
	}
}
