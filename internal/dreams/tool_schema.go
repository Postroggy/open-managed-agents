package dreams

import "encoding/json"

func toolDefinitions() []Tool {
	return []Tool{
		{Name: "read", Description: "Read source transcript or memory by Unicode-character offset. Continue using next_offset until eof. Source reads require session_id; memory reads require path. Only source IDs assigned to this stage are available. Returns sha256 for versioned writes.", InputSchema: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string","enum":["source","memory"]},"session_id":{"type":"string"},"path":{"type":"string"},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":32768}},"required":["target"],"additionalProperties":false}`)},
		{Name: "write", Description: "Write UTF-8 memory only in the output store. Use a canonical absolute store-relative path, such as /deployment.md. To overwrite, first read the entire current file and supply its sha256. Omit expected_sha256 only when creating a new path.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"},"expected_sha256":{"type":"string"}},"required":["path","content"],"additionalProperties":false}`)},
		{Name: "edit", Description: "Replace one exact occurrence after reading the full current memory. The expected_sha256 must match; ambiguous old_text is rejected. Source transcripts cannot be edited.", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old_text":{"type":"string"},"new_text":{"type":"string"},"expected_sha256":{"type":"string"}},"required":["path","old_text","new_text","expected_sha256"],"additionalProperties":false}`)},
		{Name: "glob", Description: "Discover memory paths and hashes without injecting memory bodies. Pattern * lists all paths. Other patterns use Go path.Match syntax. Paginate by next_offset until next_offset equals total.", InputSchema: json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":100}},"additionalProperties":false}`)},
		{Name: "grep", Description: "Literal case-sensitive search across memory documents; no regular expressions, no shell. Results are excerpts, not full reads; use read before overwriting. Paginate using next_offset.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"offset":{"type":"integer","minimum":0},"limit":{"type":"integer","minimum":1,"maximum":100}},"required":["query"],"additionalProperties":false}`)},
	}
}
