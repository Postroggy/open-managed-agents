package dreams

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var retirementLine = regexp.MustCompile(`^MOVED TO \[\[([a-z0-9][a-z0-9-]*)\]\]$`)
var wikiLink = regexp.MustCompile(`\[\[([^\[\]\n]+)\]\]`)
var markdownLink = regexp.MustCompile(`\[[^\]\n]*\]\(([^\)\n]+)\)`)

type memoryCatalog struct {
	pages   map[string]Memory
	slugs   map[string][]string
	retired map[string]string
}

func retiredTarget(content string) (string, bool) {
	match := retirementLine.FindStringSubmatch(strings.TrimSpace(content))
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

func pageSlug(name string) string {
	return strings.TrimSuffix(path.Base(name), path.Ext(name))
}

func (e *Engine) loadMemoryCatalog(ctx context.Context, storeID string) (memoryCatalog, error) {
	items, err := e.Backend.List(ctx, storeID)
	if err != nil {
		return memoryCatalog{}, err
	}
	if len(items) > e.Limits.MaxMemories {
		return memoryCatalog{}, outputError("document limit exceeded")
	}
	catalog := memoryCatalog{pages: map[string]Memory{}, slugs: map[string][]string{}, retired: map[string]string{}}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return memoryCatalog{}, err
		}
		if err := validPath(item.Path); err != nil {
			return memoryCatalog{}, err
		}
		if _, exists := catalog.pages[item.Path]; exists {
			return memoryCatalog{}, outputError("duplicate catalog path")
		}
		memory, err := readCatalogMemory(ctx, e.Backend, storeID, item)
		if err != nil {
			return memoryCatalog{}, err
		}
		if !utf8.ValidString(memory.Content) || len(memory.Content) > e.Limits.MaxMemoryBytes {
			return memoryCatalog{}, outputError("invalid or oversized document")
		}
		catalog.pages[item.Path] = memory
		slug := pageSlug(item.Path)
		catalog.slugs[slug] = append(catalog.slugs[slug], item.Path)
		if target, retired := retiredTarget(memory.Content); retired {
			catalog.retired[item.Path] = target
		}
	}
	return catalog, nil
}

func outputError(message string) error {
	return &Failure{Type: "invalid_output", Message: message}
}

func (c memoryCatalog) resolveWiki(slug string) (string, error) {
	paths := c.slugs[slug]
	if len(paths) != 1 {
		return "", outputError("wiki reference is missing or ambiguous: " + slug)
	}
	return paths[0], nil
}

func (c memoryCatalog) validate() error {
	if len(c.pages) == 0 {
		return nil
	}
	index, ok := c.pages["/_index.md"]
	if !ok || strings.TrimSpace(index.Content) == "" {
		return outputError("a nonempty /_index.md is required")
	}
	if _, retired := c.retired[index.Path]; retired {
		return outputError("the root index cannot be retired")
	}
	for from, slug := range c.retired {
		target, err := c.resolveWiki(slug)
		if err != nil {
			return err
		}
		if target == from || target == "/_index.md" {
			return outputError("invalid retirement destination")
		}
		if _, retired := c.retired[target]; retired {
			return outputError("retirement chains must be flattened before cleanup")
		}
		if strings.TrimSpace(c.pages[target].Content) == "" {
			return outputError("retirement destination is empty")
		}
	}
	indexLinks := map[string]bool{}
	for name, memory := range c.pages {
		if _, retired := c.retired[name]; retired {
			continue
		}
		if strings.EqualFold(path.Ext(name), ".md") {
			if err := validateFrontmatter(memory); err != nil {
				return err
			}
		}
		links, err := c.links(memory)
		if err != nil {
			return err
		}
		for _, target := range links {
			if _, retired := c.retired[target]; retired {
				return outputError("an active page links to a retired page")
			}
			if name == "/_index.md" {
				indexLinks[target] = true
			}
		}
	}
	for name := range c.pages {
		if name == "/_index.md" {
			continue
		}
		if _, retired := c.retired[name]; !retired && !indexLinks[name] {
			return outputError("index omits an active memory: " + name)
		}
	}
	return nil
}

func validateFrontmatter(memory Memory) error {
	body := strings.ReplaceAll(memory.Content, "\r\n", "\n")
	if !strings.HasPrefix(body, "---\n") {
		return nil
	}
	lines := strings.Split(body, "\n")
	fields := map[string]string{}
	closed := false
	for _, line := range lines[1:] {
		if line == "---" {
			closed = true
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || (key != "name" && key != "description") {
			continue
		}
		if _, duplicate := fields[key]; duplicate {
			return outputError("duplicate frontmatter field")
		}
		fields[key] = strings.Trim(strings.TrimSpace(value), "\"'")
	}
	if !closed {
		return outputError("unclosed frontmatter")
	}
	if memory.Path != "/_index.md" && (fields["name"] != pageSlug(memory.Path) || fields["description"] == "") {
		return outputError("frontmatter must name its file slug and describe its contents")
	}
	return nil
}

func (c memoryCatalog) links(memory Memory) ([]string, error) {
	visible := referenceText(memory.Content)
	links := []string{}
	for _, match := range wikiLink.FindAllStringSubmatch(visible, -1) {
		slug, _, _ := strings.Cut(match[1], "|")
		slug, _, _ = strings.Cut(slug, "#")
		target, err := c.resolveWiki(strings.TrimSpace(slug))
		if err != nil {
			return nil, err
		}
		links = append(links, target)
	}
	for _, match := range markdownLink.FindAllStringSubmatch(visible, -1) {
		target, err := c.resolveMarkdown(memory.Path, match[1])
		if err != nil {
			return nil, err
		}
		if target != "" {
			links = append(links, target)
		}
	}
	return links, nil
}

func (c memoryCatalog) resolveMarkdown(from, raw string) (string, error) {
	href := strings.TrimSpace(raw)
	if href == "" {
		return "", outputError("empty markdown reference")
	}
	if strings.HasPrefix(href, "<") {
		end := strings.IndexByte(href, '>')
		if end < 0 {
			return "", outputError("unclosed markdown reference")
		}
		href = href[1:end]
	} else {
		href = strings.Fields(href)[0]
	}
	parsed, err := url.Parse(href)
	if err != nil {
		return "", outputError("malformed markdown reference")
	}
	if parsed.Scheme != "" || parsed.Host != "" || parsed.Path == "" {
		return "", nil
	}
	target := parsed.Path
	if !strings.HasPrefix(target, "/") {
		target = path.Join(path.Dir(from), target)
	}
	if err := validPath(target); err != nil {
		return "", outputError("invalid local markdown reference")
	}
	if _, exists := c.pages[target]; !exists {
		if from != "/_index.md" {
			return "", nil
		}
		return "", outputError("index reference is missing: " + target)
	}
	return target, nil
}

func referenceText(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	visible := make([]string, 0, len(lines))
	fence := ""
	frontmatter := len(lines) > 0 && lines[0] == "---"
	for i, line := range lines {
		if frontmatter {
			if i > 0 && line == "---" {
				frontmatter = false
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, string(fence[0])) == "" {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			char := trimmed[0]
			end := 0
			for end < len(trimmed) && trimmed[end] == char {
				end++
			}
			fence = trimmed[:end]
			continue
		}
		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}
		visible = append(visible, stripInlineCode(line))
	}
	return strings.Join(visible, "\n")
}

func stripInlineCode(line string) string {
	var result strings.Builder
	for i := 0; i < len(line); {
		if line[i] == '\\' && i+1 < len(line) {
			result.WriteByte(' ')
			i += 2
			continue
		}
		if line[i] != '`' {
			result.WriteByte(line[i])
			i++
			continue
		}
		end := i
		for end < len(line) && line[end] == '`' {
			end++
		}
		marker := line[i:end]
		closeAt := strings.Index(line[end:], marker)
		if closeAt < 0 {
			result.WriteString(line[i:])
			break
		}
		result.WriteByte(' ')
		i = end + closeAt + len(marker)
	}
	return result.String()
}

func (e *Engine) inspectAndCleanOutput(ctx context.Context, storeID string) error {
	catalog, err := e.loadMemoryCatalog(ctx, storeID)
	if err != nil {
		return err
	}
	if err := catalog.validate(); err != nil {
		return err
	}
	if len(catalog.retired) == 0 {
		return nil
	}
	deleter, ok := e.Backend.(RetiredMemoryDeleter)
	if !ok {
		return &Failure{Type: "retirement_cleanup_unavailable", Message: "Backend does not support conditional retirement cleanup"}
	}
	paths := make([]string, 0, len(catalog.retired))
	for name := range catalog.retired {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	for _, name := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := catalog.pages[name]
		target, _ := catalog.resolveWiki(catalog.retired[name])
		if _, err := readCatalogMemory(ctx, e.Backend, storeID, catalog.pages[target]); err != nil {
			return err
		}
		if err := deleter.DeleteRetiredMemory(ctx, storeID, item); err != nil {
			return fmt.Errorf("conditional retirement cleanup: %w", err)
		}
		if err := e.record(ctx, Event{StoreID: storeID, Stage: "cleanup", Kind: "memory_retired", Path: name, SHA256: item.SHA256}); err != nil {
			return err
		}
	}
	final, err := e.loadMemoryCatalog(ctx, storeID)
	if err != nil {
		return err
	}
	if len(final.retired) != 0 {
		return outputError("retirement cleanup left unresolved stubs")
	}
	return final.validate()
}

func (t *toolkit) validateRetirement(ctx context.Context, a toolArgs) error {
	slug, retired := retiredTarget(a.Content)
	if !retired {
		return nil
	}
	if !t.organizer || a.ExpectedSHA256 == "" || a.Path == "/_index.md" {
		return outputError("only the organizer can retire an existing topic page")
	}
	items, err := t.backend.List(ctx, t.storeID)
	if err != nil {
		return err
	}
	var destinations []Memory
	for _, item := range items {
		if pageSlug(item.Path) == slug {
			destinations = append(destinations, item)
		}
	}
	if len(destinations) != 1 || destinations[0].Path == a.Path || destinations[0].Path == "/_index.md" {
		return outputError("retirement destination is missing, ambiguous or self-referencing")
	}
	destination, err := readCatalogMemory(ctx, t.backend, t.storeID, destinations[0])
	if err != nil {
		return err
	}
	_, isStub := retiredTarget(destination.Content)
	if isStub || strings.TrimSpace(destination.Content) == "" || t.versions[destination.Path] != destination.SHA256 || t.unverified[destination.Path] != "" {
		return outputError("read the complete non-retired destination and verify the merged facts before retiring a page")
	}
	return nil
}
