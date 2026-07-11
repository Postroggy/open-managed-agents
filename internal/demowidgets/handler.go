package demowidgets

import "net/http"

// Handler is a smoke stub for private-fork docs-sync E2E.
// It exposes GET /demo_widgets so the surface audit sees an unmapped API mount
// and package that the docs-sync agent should document or map.
type Handler struct{}

func New() *Handler { return &Handler{} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/" && r.URL.Path != "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"type":"demo_widget_list","data":[],"note":"docs-sync smoke stub"}`))
}
