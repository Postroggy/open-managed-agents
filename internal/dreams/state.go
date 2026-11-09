package dreams

import (
	"errors"
	"time"
)

type Status string

const (
	Pending   Status = "pending"
	Running   Status = "running"
	Completed Status = "completed"
	Failed    Status = "failed"
	Canceled  Status = "canceled"
)

type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

func (u Usage) Add(v Usage) Usage {
	return Usage{u.InputTokens + v.InputTokens, u.OutputTokens + v.OutputTokens, u.CacheReadInputTokens + v.CacheReadInputTokens, u.CacheCreationInputTokens + v.CacheCreationInputTokens}
}

type Failure struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (f *Failure) Error() string { return f.Type + ": " + f.Message }

type Output struct {
	Type          string `json:"type"`
	MemoryStoreID string `json:"memory_store_id"`
}

type Dream struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Request
	Status     Status     `json:"status"`
	Outputs    []Output   `json:"outputs"`
	SessionID  *string    `json:"session_id"`
	CreatedAt  time.Time  `json:"created_at"`
	EndedAt    *time.Time `json:"ended_at"`
	ArchivedAt *time.Time `json:"archived_at"`
	Usage      Usage      `json:"usage"`
	Error      *Failure   `json:"error"`
}

var ErrState = errors.New("invalid dream state transition")

func New(id string, req Request, now time.Time) (Dream, error) {
	normalized, err := Normalize(req)
	if err != nil {
		return Dream{}, err
	}
	if !validID(id) {
		return Dream{}, invalid("invalid dream ID")
	}
	return Dream{Type: "dream", ID: id, Request: normalized, Status: Pending, Outputs: []Output{}, CreatedAt: now.UTC()}, nil
}

func (d *Dream) Start() error {
	if d.Status != Pending {
		return ErrState
	}
	d.Status = Running
	return nil
}

func (d *Dream) Finish(status Status, failure *Failure, now time.Time) error {
	if d.Status == Canceled {
		return nil
	}
	if d.Status != Running || (status != Completed && status != Failed) {
		return ErrState
	}
	if (status == Failed) != (failure != nil) {
		return ErrState
	}
	t := now.UTC()
	d.Status, d.Error, d.EndedAt = status, failure, &t
	return nil
}

func (d *Dream) Cancel(now time.Time) error {
	switch d.Status {
	case Canceled:
		return nil
	case Pending, Running:
		t := now.UTC()
		d.Status, d.EndedAt = Canceled, &t
		return nil
	default:
		return ErrState
	}
}

func (d *Dream) Archive(now time.Time) error {
	if !d.Terminal() {
		return ErrState
	}
	if d.ArchivedAt == nil {
		t := now.UTC()
		d.ArchivedAt = &t
	}
	return nil
}

func (d Dream) Terminal() bool {
	return d.Status == Completed || d.Status == Failed || d.Status == Canceled
}
