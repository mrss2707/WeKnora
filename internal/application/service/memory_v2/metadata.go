package memory_v2

import "encoding/json"

// metadataEmbeddingModel is the agent_memories.metadata key recording which
// embedding model produced the row's vector.
const metadataEmbeddingModel = "embedding_model_id"

// setMetadataString returns raw JSON metadata with key set to value, keeping
// every other key. Invalid or empty input starts from an empty object.
func setMetadataString(raw []byte, key, value string) []byte {
	m := map[string]interface{}{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &m)
		if m == nil {
			m = map[string]interface{}{}
		}
	}
	m[key] = value
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return out
}

// metadataString reads a string key from raw JSON metadata.
func metadataString(raw []byte, key string) string {
	if len(raw) == 0 {
		return ""
	}
	m := map[string]interface{}{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}
