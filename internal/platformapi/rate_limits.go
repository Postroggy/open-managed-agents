package platformapi

import (
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"

	"github.com/go-chi/chi/v5"
)

var platformClaudeLegacyRateLimitModelGroups = []string{
	"claude_fable_5",
	"claude_haiku_4",
	"claude_opus_4_5",
	"claude_sonnet_4",
}

const (
	platformClaudeFastInputTokensPerMinute  = 10000
	platformClaudeFastOutputTokensPerMinute = 4000
)

func handleRateLimits(w http.ResponseWriter, _ *http.Request) {
	limiters := make([]map[string]any, 0, 14)
	for _, modelGroup := range platformClaudeLegacyRateLimitModelGroups {
		for _, limit := range platformClaudeLegacyRateLimitsForModelGroup(modelGroup) {
			limiters = append(limiters, map[string]any{
				"limiter":     limit["type"],
				"value":       limit["value"],
				"source":      "default",
				"model_group": modelGroup,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rate_limit_tier":              "auto_api_evaluation",
		"tier_model_rate_limiters":     limiters,
		"custom_default_rate_limiters": nil,
		"custom_model_rate_limiters":   nil,
		"spend_threshold":              50000,
		"effective_rate_limiters":      nil,
	})
}

func visibleOrgUUIDOrPlatformClaudeMirror(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := platformClaudeMirrorOrgUUID(r); ok {
		return true
	}
	_, ok := visibleOrgUUID(w, r)
	return ok
}

func platformClaudeMirrorOrgUUID(r *http.Request) (string, bool) {
	orgUUID := strings.TrimSpace(chi.URLParam(r, "orgUuid"))
	principal, ok := auth.PrincipalFromContext(r.Context())
	if orgUUID == "" || !ok || !principalCanSeeOrg(principal, orgUUID) || !isPlatformClaudeHost(r.Host) {
		return "", false
	}
	return orgUUID, true
}

func platformClaudeLegacyRateLimitsForModelGroup(modelGroup string) []map[string]any {
	if modelGroup == "claude_opus_4_5" {
		return append([]map[string]any{
			platformClaudeRateLimit("fast_itpmca", platformClaudeFastInputTokensPerMinute),
			platformClaudeRateLimit("fast_otpm", platformClaudeFastOutputTokensPerMinute),
		}, platformStandardRateLimitMaps()...)
	}
	return platformStandardRateLimitMaps()
}

func platformStandardRateLimitMaps() []map[string]any {
	return []map[string]any{
		platformClaudeRateLimit("input_tokens_per_minute_cache_aware", 10000),
		platformClaudeRateLimit("output_tokens_per_minute", 4000),
		platformClaudeRateLimit("requests_per_minute", 5),
	}
}

func platformClaudeRateLimit(limitType string, value int) map[string]any {
	return map[string]any{"type": limitType, "value": value, "multiplier_config": nil}
}
