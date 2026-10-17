package memory_v2

import "encoding/json"

// Audits predating metadata snapshots can only remove this merge's IDs from
// merged_from. Preserve every unrelated metadata key rather than erase it.
func legacyUndoMetadata(current []byte, restoredIDs []string) []byte {
	values := map[string]interface{}{}
	_ = json.Unmarshal(current, &values)
	if values == nil {
		values = map[string]interface{}{}
	}
	remove := map[string]bool{}
	for _, id := range restoredIDs {
		remove[id] = true
	}
	if ids, ok := values["merged_from"].([]interface{}); ok {
		kept := []interface{}{}
		for _, value := range ids {
			id, _ := value.(string)
			if !remove[id] {
				kept = append(kept, value)
			}
		}
		values["merged_from"] = kept
	}
	raw, _ := json.Marshal(values)
	return raw
}
