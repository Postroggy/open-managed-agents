package dreams

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func catalogFixture(t *testing.T, pages map[string]string) (*Engine, *fakeBackend) {
	t.Helper()
	b := fixtureBackend()
	b.stores["output"] = map[string]Memory{}
	for p, s := range pages {
		b.stores["output"][p] = Memory{ID: "mem_" + digest(p)[:12], Path: p, Content: s, SHA256: digest(s)}
	}
	return &Engine{Backend: b, Limits: DefaultLimits()}, b
}
func TestWikiValidationRejectsStructuralLoss(t *testing.T) {
	cases := []struct {
		name  string
		pages map[string]string
	}{
		{"missing index", map[string]string{"/fact.md": "fact"}},
		{"empty index", map[string]string{"/_index.md": " ", "/fact.md": "fact"}},
		{"orphan", map[string]string{"/_index.md": "[[fact]]", "/fact.md": "fact", "/orphan.md": "irreplaceable fact"}},
		{"broken wiki", map[string]string{"/_index.md": "[[fact]]", "/fact.md": "See [[missing]]"}},
		{"broken markdown", map[string]string{"/_index.md": "[Fact](fact.md)\n[missing](missing.md)", "/fact.md": "Fact"}},
		{"duplicate slug", map[string]string{"/_index.md": "[[fact]]", "/a/fact.md": "first", "/b/fact.md": "second"}},
		{"bad frontmatter", map[string]string{"/_index.md": "[[fact]]", "/fact.md": "---\nname: wrong\ndescription: topic\n---\nFact"}},
		{"duplicate frontmatter", map[string]string{"/_index.md": "[[fact]]", "/fact.md": "---\nname: fact\nname: other\ndescription: topic\n---\nFact"}},
		{"missing retirement target", map[string]string{"/_index.md": "[[fact]]", "/fact.md": "Fact", "/old.md": "MOVED TO [[missing]]"}},
		{"retirement cycle", map[string]string{"/_index.md": "[[fact]]", "/fact.md": "Fact", "/a.md": "MOVED TO [[b]]", "/b.md": "MOVED TO [[a]]"}},
		{"retirement chain", map[string]string{"/_index.md": "[[fact]]", "/fact.md": "Fact", "/a.md": "MOVED TO [[b]]", "/b.md": "MOVED TO [[fact]]"}},
		{"indexed stub", map[string]string{"/_index.md": "[[fact]]\n[[old]]", "/fact.md": "Fact", "/old.md": "MOVED TO [[fact]]"}},
		{"link to stub", map[string]string{"/_index.md": "[[fact]]", "/fact.md": "Fact; see [[old]]", "/old.md": "MOVED TO [[fact]]"}},
		{"empty destination", map[string]string{"/_index.md": "[[fact]]", "/fact.md": "", "/old.md": "MOVED TO [[fact]]"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := catalogFixture(t, tc.pages)
			err := e.validateOutput(context.Background(), "output")
			var failure *Failure
			if !errors.As(err, &failure) || failure.Type != "invalid_output" {
				t.Fatalf("wanted invalid_output, got %v", err)
			}
		})
	}
}
func TestWikiValidationAcceptsLegacyAndWikiLinks(t *testing.T) {
	e, _ := catalogFixture(t, map[string]string{
		"/_index.md":        "## Domain\n[[fact]] — fact\n[Second](nested/second.md#details)",
		"/fact.md":          "---\nname: fact\ndescription: Verified facts\n---\nSee [[second]].\n```md\n[[not-a-real-link]]\n```\nInline `[[another-example]]` and [web](https://example.com/missing) and \\[[escaped-example]].",
		"/nested/second.md": "[fact](../fact.md)",
	})
	if err := e.validateOutput(context.Background(), "output"); err != nil {
		t.Fatal(err)
	}
}

type deletingBackend struct {
	*fakeBackend
	deleted      []string
	beforeDelete func(Memory)
}

func (b *deletingBackend) DeleteRetiredMemory(ctx context.Context, store string, m Memory) error {
	if b.beforeDelete != nil {
		b.beforeDelete(m)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	old, ok := b.stores[store][m.Path]
	if !ok || old.SHA256 != m.SHA256 {
		return errors.New("precondition failed")
	}
	if _, ok := retiredTarget(old.Content); !ok {
		return errors.New("not a stub")
	}
	delete(b.stores[store], m.Path)
	b.deleted = append(b.deleted, m.Path)
	return nil
}
func TestRetirementCleanupDeletesOnlyVerifiedStubs(t *testing.T) {
	e, b := catalogFixture(t, map[string]string{"/_index.md": "[[fact]]", "/fact.md": "Fact one. Fact two.", "/old.md": "MOVED TO [[fact]]\n"})
	backend := &deletingBackend{fakeBackend: b}
	e.Backend = backend
	if err := e.validateOutput(context.Background(), "output"); err != nil {
		t.Fatal(err)
	}
	if len(backend.deleted) != 1 || backend.deleted[0] != "/old.md" {
		t.Fatal(backend.deleted)
	}
	if b.stores["output"]["/fact.md"].Content != "Fact one. Fact two." {
		t.Fatal("destination changed")
	}
	if err := e.validateOutput(context.Background(), "output"); err != nil {
		t.Fatal("repeat cleanup:", err)
	}
}
func TestRetirementCleanupDoesNotDeleteChangedContent(t *testing.T) {
	e, b := catalogFixture(t, map[string]string{"/_index.md": "[[fact]]", "/fact.md": "Facts", "/old.md": "MOVED TO [[fact]]"})
	backend := &deletingBackend{fakeBackend: b}
	e.Backend = backend
	backend.beforeDelete = func(m Memory) {
		b.mu.Lock()
		b.stores["output"][m.Path] = Memory{ID: m.ID, Path: m.Path, Content: "New unique fact", SHA256: digest("New unique fact")}
		b.mu.Unlock()
	}
	if err := e.validateOutput(context.Background(), "output"); err == nil {
		t.Fatal("concurrent mutation lost")
	}
	if len(backend.deleted) != 0 || b.stores["output"]["/old.md"].Content != "New unique fact" {
		t.Fatal("lost content")
	}
}
func TestRetirementNeverBeginsForInvalidIndex(t *testing.T) {
	e, b := catalogFixture(t, map[string]string{"/_index.md": "[[fact]]\n[[old]]", "/fact.md": "Fact", "/old.md": "MOVED TO [[fact]]"})
	backend := &deletingBackend{fakeBackend: b}
	e.Backend = backend
	if err := e.validateOutput(context.Background(), "output"); err == nil {
		t.Fatal("invalid index accepted")
	}
	if len(backend.deleted) != 0 {
		t.Fatal("cleanup preceded validation")
	}
}
func TestRetirementRequiresOrganizerAndReadBackDestination(t *testing.T) {
	_, b := catalogFixture(t, map[string]string{"/old.md": "Old fact", "/fact.md": "Old fact plus new fact"})
	kit := newToolkit(b, "output", nil, DefaultLimits())
	_, _ = executeTool(t, kit, "read", toolArgs{Target: "memory", Path: "/old.md"})
	retire := toolArgs{Path: "/old.md", Content: "MOVED TO [[fact]]", ExpectedSHA256: digest("Old fact")}
	if _, err := executeTool(t, kit, "write", retire); err == nil {
		t.Fatal("analyzer retired a file")
	}
	kit.organizer = true
	if _, err := executeTool(t, kit, "write", retire); err == nil {
		t.Fatal("unread destination accepted")
	}
	if _, err := executeTool(t, kit, "read", toolArgs{Target: "memory", Path: "/fact.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := executeTool(t, kit, "write", retire); err != nil {
		t.Fatal(err)
	}
	if len(kit.unverified) != 1 {
		t.Fatal("stub write has no verification obligation")
	}
	if _, err := executeTool(t, kit, "read", toolArgs{Target: "memory", Path: "/old.md"}); err != nil {
		t.Fatal(err)
	}
	if len(kit.unverified) != 0 {
		t.Fatal("stub read-back not registered")
	}
}
func TestEachWriteRequiresFreshCompleteReadBack(t *testing.T) {
	b := fixtureBackend()
	kit := newToolkit(b, "memstore_source", nil, DefaultLimits())
	old := digest("Old claim: east")
	_, _ = executeTool(t, kit, "read", toolArgs{Target: "memory", Path: "/note.md"})
	_, err := executeTool(t, kit, "write", toolArgs{Path: "/note.md", Content: "Old claim: east", ExpectedSHA256: old})
	if err != nil {
		t.Fatal(err)
	}
	if len(kit.unverified) != 1 {
		t.Fatal("no-op write incorrectly inherited old verification")
	}
	_, _ = executeTool(t, kit, "read", toolArgs{Target: "memory", Path: "/note.md", Limit: 1})
	if len(kit.unverified) != 1 {
		t.Fatal("partial verification accepted")
	}
	_, _ = executeTool(t, kit, "read", toolArgs{Target: "memory", Path: "/note.md", Offset: 1})
	if len(kit.unverified) != 0 {
		t.Fatal("complete verification rejected")
	}
}
func TestInputDeletionAfterModelStopsBeforeWrites(t *testing.T) {
	for _, id := range []string{"sesn_a", "sesn_b"} {
		t.Run(id, func(t *testing.T) {
			b := fixtureBackend()
			d := newDream(t)
			e := Engine{Backend: b, Limits: DefaultLimits(), Model: modelFunc(func(context.Context, ModelRequest) (ModelResponse, error) {
				b.mu.Lock()
				delete(b.transcripts, id)
				b.mu.Unlock()
				return toolResponse("bad", "write", toolArgs{Path: "/bad.md", Content: "must not commit"}), nil
			})}
			err := e.Run(context.Background(), d)
			if err == nil || d.Error.Type != "input_session_unavailable" {
				t.Fatalf("%+v %v", d, err)
			}
			if _, exists := b.stores[d.Outputs[0].MemoryStoreID]["/bad.md"]; exists {
				t.Fatal("wrote after source deletion")
			}
		})
	}
}
func TestExplicitNullOutputAndEmptySpeedRejected(t *testing.T) {
	base := `{"model":"m","inputs":[{"type":"memory_store","memory_store_id":"store"},{"type":"sessions","session_ids":["session"]}]}`
	for _, extra := range []string{`"output_behavior":null,`, `"output_behavior":{"type":"create_new","extra":1},`} {
		if _, err := DecodeRequest(strings.NewReader(strings.Replace(base, `"model":`, extra+`"model":`, 1))); err == nil {
			t.Fatal(extra)
		}
	}
	if _, err := DecodeRequest(strings.NewReader(strings.Replace(base, `"m"`, `{"id":"m","speed":""}`, 1))); err == nil {
		t.Fatal("empty speed accepted")
	}
	for _, speed := range []string{`null`, `"standard"`} {
		r, err := DecodeRequest(strings.NewReader(strings.Replace(base, `"m"`, fmt.Sprintf(`{"id":"m","speed":%s}`, speed), 1)))
		if err != nil || r.Model.Speed != "standard" {
			t.Fatalf("%+v %v", r, err)
		}
	}
}

func TestReadBackMustObserveAWriteInALaterTurn(t *testing.T) {
	b := fixtureBackend()
	kit := newToolkit(b, "memstore_source", nil, DefaultLimits())
	kit.turn = 1
	_, _, err := kit.execute(context.Background(), "write", []byte(`{"path":"/new.md","content":"verified"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = kit.execute(context.Background(), "read", []byte(`{"target":"memory","path":"/new.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(kit.unverified) != 1 {
		t.Fatal("same-response tool batch counted as post-write verification")
	}
	kit.turn = 2
	_, _, err = kit.execute(context.Background(), "read", []byte(`{"target":"memory","path":"/new.md"}`))
	if err != nil || len(kit.unverified) != 0 {
		t.Fatalf("read-back not accepted: %v", err)
	}
}

type mutateAfterCopyBackend struct {
	*fakeBackend
	mutate func()
}

func (b *mutateAfterCopyBackend) Write(ctx context.Context, store string, m Memory, expected string) (Memory, error) {
	written, err := b.fakeBackend.Write(ctx, store, m, expected)
	if err == nil && store != "memstore_source" && b.mutate != nil {
		mutate := b.mutate
		b.mutate = nil
		mutate()
	}
	return written, err
}
func TestCopyDetectsSourceCatalogChanges(t *testing.T) {
	for _, mutation := range []string{"added", "deleted", "modified"} {
		t.Run(mutation, func(t *testing.T) {
			base := fixtureBackend()
			b := &mutateAfterCopyBackend{fakeBackend: base}
			b.mutate = func() {
				base.mu.Lock()
				defer base.mu.Unlock()
				switch mutation {
				case "added":
					base.stores["memstore_source"]["/extra.md"] = Memory{Path: "/extra.md", Content: "new", SHA256: digest("new")}
				case "deleted":
					delete(base.stores["memstore_source"], "/note.md")
				case "modified":
					m := base.stores["memstore_source"]["/note.md"]
					m.Content = "changed"
					m.SHA256 = digest(m.Content)
					base.stores["memstore_source"]["/note.md"] = m
				}
			}
			d := newDream(t)
			e := Engine{Backend: b, Model: scriptedModel(t), Limits: DefaultLimits()}
			if err := e.Run(context.Background(), d); err == nil || d.Error == nil || d.Error.Type != "input_memory_store_changed" {
				t.Fatalf("%v %+v", err, d)
			}
			if len(d.Outputs) != 1 {
				t.Fatal("partial output lost")
			}
		})
	}
}

func TestBodyProjectLinksAreNotInventedMemoryDependencies(t *testing.T) {
	e, _ := catalogFixture(t, map[string]string{
		"/_index.md":       "[Source](<source notes.md>)\n[[fact]]",
		"/source notes.md": "Read [deployment](../deploy/config.yaml) in the source checkout.",
		"/fact.md":         "---\r\nname: fact\r\ndescription: Source references\r\n---\r\nSee [code](src/main.go), [paper](https://example.org/a), and [[source notes]].",
	})
	if err := e.validateOutput(context.Background(), "output"); err != nil {
		t.Fatal(err)
	}
}
