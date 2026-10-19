package workers

import "encoding/json"

// dreamerResponseFormat places confidence and evidence on each action, never
// on the root object. The schema allows low confidence; the unchanged server
// confidence gate, scope checks and mutation guards remain authoritative.
func dreamerResponseFormat(batchSize, maxActions int, allowRemove bool) json.RawMessage {
	index := map[string]any{"type": "integer", "minimum": 1, "maximum": max(1, batchSize)}
	branch := func(kind string, extra map[string]any, required ...string) map[string]any {
		properties := map[string]any{
			"type":       map[string]any{"const": kind},
			"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"reason":     map[string]any{"type": "string", "minLength": 1},
		}
		for name, value := range extra {
			properties[name] = value
		}
		return map[string]any{
			"type": "object", "additionalProperties": false, "properties": properties,
			"required": append([]string{"type", "confidence", "reason"}, required...),
		}
	}
	variants := []map[string]any{
		branch("update_verdict", map[string]any{
			"index": index, "new_verdict": map[string]any{"type": "string", "enum": []string{"none", "refuted", "gotcha", "wip"}},
		}, "index", "new_verdict"),
		branch("adjust_importance", map[string]any{
			"index": index, "delta": map[string]any{"type": "integer", "enum": []int{-1, 1}},
		}, "index", "delta"),
	}
	if batchSize >= 2 {
		variants = append(variants, branch("merge", map[string]any{
			"indexes": map[string]any{"type": "array", "minItems": 2, "maxItems": 2, "uniqueItems": true, "items": index},
		}, "indexes"))
	}
	if allowRemove {
		variants = append(variants, branch("remove", map[string]any{"index": index}, "index"))
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"actions"},
		"properties": map[string]any{"actions": map[string]any{
			"type": "array", "maxItems": max(1, min(maxActions, batchSize)), "items": map[string]any{"oneOf": variants},
		}},
	}
	encoded, _ := json.Marshal(schema)
	return encoded
}
