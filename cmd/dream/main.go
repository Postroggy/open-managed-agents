package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/dreamclient"
	"github.com/superduck-ai/open-managed-agents/internal/dreams"
)

type journal struct {
	file       *os.File
	dream      *dreams.Dream
	resultPath string
}

func (j *journal) Record(_ context.Context, event dreams.Event) error {
	if err := json.NewEncoder(j.file).Encode(event); err != nil {
		return err
	}
	if err := j.file.Sync(); err != nil {
		return err
	}
	return writeResult(j.resultPath, j.dream)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger, os.Args[1:]); err != nil {
		logger.Error("dream stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("dream", flag.ContinueOnError)
	requestFile := flags.String("request", "", "Dream create request JSON file (required)")
	baseURL := flags.String("base-url", "http://localhost:38080", "OMA service root URL")
	records := flags.String("records-dir", "", "New private run directory; defaults to a temporary directory")
	insecure := flags.Bool("allow-insecure-http", false, "Allow HTTP for an explicitly trusted local OMA endpoint")
	execute := flags.Bool("execute", false, "Authorize model calls and creation of an output store; without this flag validate only")
	limits := dreams.DefaultLimits()
	flags.DurationVar(&limits.Timeout, "timeout", limits.Timeout, "Maximum total run duration")
	flags.IntVar(&limits.MaxTurns, "max-turns", limits.MaxTurns, "Maximum model turns per analyzer/organizer")
	flags.Int64Var(&limits.MaxTotalTokens, "max-tokens", limits.MaxTotalTokens, "Stop between responses when aggregate token usage reaches this bound (not a dollar cap)")
	flags.IntVar(&limits.MaxContextBytes, "max-context-bytes", limits.MaxContextBytes, "Maximum serialized Messages request bytes")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if *requestFile == "" || flags.NArg() != 0 {
		return errors.New("supply -request FILE; -execute is required for side effects")
	}
	file, err := os.Open(*requestFile)
	if err != nil {
		return err
	}
	request, err := dreams.DecodeRequest(io.LimitReader(file, 1<<20))
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if request.OutputBehavior.Type == "update_existing" {
		return dreams.ErrUpdateRequiresCoordinator
	}
	if !*execute {
		return json.NewEncoder(os.Stdout).Encode(request)
	}
	client, err := dreamclient.New(dreamclient.Options{BaseURL: *baseURL, APIKey: os.Getenv("OMA_API_KEY"), WorkspaceID: os.Getenv("OMA_WORKSPACE_ID"), AllowInsecure: *insecure})
	if err != nil {
		return err
	}
	runDir, err := prepareDirectory(*records)
	if err != nil {
		return err
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return err
	}
	dream, err := dreams.New("drm_"+hex.EncodeToString(idBytes), request, time.Now())
	if err != nil {
		return err
	}
	resultPath := filepath.Join(runDir, "result.json")
	if err := writeResult(resultPath, &dream); err != nil {
		return err
	}
	audit, err := os.OpenFile(filepath.Join(runDir, "events.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer audit.Close()
	logger.Info("starting standalone dreaming run", "id", dream.ID, "records", runDir, "mode", "create_new")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	engine := dreams.Engine{Backend: client, Model: client, Observer: &journal{file: audit, dream: &dream, resultPath: resultPath}, Limits: limits}
	runErr := engine.Run(ctx, &dream)
	saveErr := writeResult(resultPath, &dream)
	if err := json.NewEncoder(os.Stdout).Encode(dream); err != nil {
		return errors.Join(runErr, saveErr, err)
	}
	logger.Info("dream run ended", "status", dream.Status, "result", resultPath)
	return errors.Join(runErr, saveErr)
}

func prepareDirectory(name string) (string, error) {
	if name == "" {
		return os.MkdirTemp("", "oma-dream-")
	}
	if err := os.Mkdir(name, 0700); err != nil {
		return "", fmt.Errorf("create new private records directory: %w", err)
	}
	return name, nil
}

func writeResult(name string, dream *dreams.Dream) error {
	data, err := json.MarshalIndent(dream, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(name), ".dream-result-")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err = temporary.Write(append(data, '\n')); err == nil {
		err = temporary.Sync()
	}
	err = errors.Join(err, temporary.Close())
	if err != nil {
		return err
	}
	return os.Rename(temporary.Name(), name)
}
