package dreams

import (
	"context"
	"errors"
)

type InputValidator interface {
	ValidateInputs(context.Context, string, []string) error
}

type CatalogReader interface {
	ReadMemory(context.Context, string, Memory) (Memory, error)
}

type RetiredMemoryDeleter interface {
	DeleteRetiredMemory(context.Context, string, Memory) error
}

func (e *Engine) validateInputs(ctx context.Context, d *Dream) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if validator, ok := e.Backend.(InputValidator); ok {
		return validator.ValidateInputs(ctx, d.Inputs[0].MemoryStoreID, d.Inputs[1].SessionIDs)
	}
	if _, err := e.Backend.Store(ctx, d.Inputs[0].MemoryStoreID); err != nil {
		return inputFailure(ctx, "input_memory_store_unavailable", "Input memory store is unavailable")
	}
	for _, id := range d.Inputs[1].SessionIDs {
		source, err := e.Backend.Transcript(ctx, id)
		if err != nil || source.SessionID != id {
			return inputFailure(ctx, "input_session_unavailable", "An input session is unavailable")
		}
	}
	return nil
}

func readCatalogMemory(ctx context.Context, backend Backend, storeID string, item Memory) (Memory, error) {
	var memory Memory
	var err error
	if reader, ok := backend.(CatalogReader); ok {
		memory, err = reader.ReadMemory(ctx, storeID, item)
	} else {
		memory, err = backend.Read(ctx, storeID, item.Path)
	}
	if err != nil {
		return Memory{}, err
	}
	if memory.Path != item.Path || (item.ID != "" && memory.ID != item.ID) {
		return Memory{}, errors.New("memory identity changed during catalog read")
	}
	if memory.SHA256 == "" || digest(memory.Content) != memory.SHA256 {
		return Memory{}, errors.New("memory content hash mismatch")
	}
	if item.SHA256 != "" && memory.SHA256 != item.SHA256 {
		return Memory{}, errors.New("memory changed during catalog read")
	}
	return memory, nil
}

type manifestEntry struct {
	ID     string
	SHA256 string
}

func memoryManifest(items []Memory) (map[string]manifestEntry, error) {
	result := make(map[string]manifestEntry, len(items))
	for _, m := range items {
		if err := validPath(m.Path); err != nil {
			return nil, err
		}
		if _, exists := result[m.Path]; exists {
			return nil, errors.New("duplicate path in source manifest")
		}
		if len(m.SHA256) != 64 {
			return nil, errors.New("source manifest requires content hashes")
		}
		result[m.Path] = manifestEntry{m.ID, m.SHA256}
	}
	return result, nil
}
func changedInputStore() error {
	return &Failure{Type: "input_memory_store_changed", Message: "Input changed during copying; retry from a stable store"}
}
