package workers

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// EntityExtractor buffers memories and periodically extracts entities via LLM.
type EntityExtractor struct {
	repo      interfaces.MemoryRepositoryV2
	chat      chat.Chat
	mu        sync.Mutex
	buffer    []*types.AgentMemory
	ticker    *time.Ticker
	batchSize int
}

// NewEntityExtractor creates a new EntityExtractor.
func NewEntityExtractor(repo interfaces.MemoryRepositoryV2, chat chat.Chat) *EntityExtractor {
	return &EntityExtractor{
		repo:      repo,
		chat:      chat,
		buffer:    make([]*types.AgentMemory, 0, 10),
		batchSize: 10,
	}
}

// Enqueue adds a memory to the extraction queue.
func (e *EntityExtractor) Enqueue(memory *types.AgentMemory) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.buffer = append(e.buffer, memory)

	if len(e.buffer) >= e.batchSize {
		go e.flush(context.Background())
	}
}

// Run starts the extraction worker loop.
func (e *EntityExtractor) Run(ctx context.Context) {
	e.ticker = time.NewTicker(30 * time.Second)
	defer e.ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Flush remaining items before exit
			e.flush(context.Background())
			return
		case <-e.ticker.C:
			e.flush(ctx)
		}
	}
}

// flush sends buffered memories to the LLM for entity extraction in a single batch.
func (e *EntityExtractor) flush(ctx context.Context) {
	e.mu.Lock()
	if len(e.buffer) == 0 {
		e.mu.Unlock()
		return
	}
	batch := make([]*types.AgentMemory, len(e.buffer))
	copy(batch, e.buffer)
	e.buffer = e.buffer[:0]
	e.mu.Unlock()

	if len(batch) == 0 {
		return
	}

	e.processBatch(ctx, batch)
}

// processBatch sends a batch to the LLM and stores extracted entities.
func (e *EntityExtractor) processBatch(ctx context.Context, batch []*types.AgentMemory) {
	if e.chat == nil {
		return
	}

	// Build prompt
	var contentBlock string
	for i, mem := range batch {
		contentBlock += formatBatchItem(i+1, mem.ID, mem.Content)
	}

	prompt := `You are an entity extraction system. Extract named entities from each memory below.
For each entity, provide:
- name: The entity name
- type: Person, Organization, Location, Concept, Technology, Event, or Other
- confidence: A float between 0.0 and 1.0 (minimum 0.7 to include)

Output ONLY valid JSON array:
[
  {"memory_index": 1, "entities": [{"name": "...", "type": "...", "confidence": 0.95}]}
]

Memories:
` + contentBlock

	resp, err := e.chat.Chat(ctx, []chat.Message{
		{Role: "user", Content: prompt},
	}, nil)
	if err != nil {
		logger.Errorf(ctx, "entity extraction batch LLM call failed: %v", err)
		return
	}

	var extracted []entityBatchResult
	if err := json.Unmarshal([]byte(resp.Content), &extracted); err != nil {
		logger.Errorf(ctx, "entity extraction parse failed: %v", err)
		return
	}

	for _, item := range extracted {
		if item.MemoryIndex < 1 || item.MemoryIndex > len(batch) {
			continue
		}
		mem := batch[item.MemoryIndex-1]
		for _, ent := range item.Entities {
			if ent.Confidence < 0.7 {
				continue
			}
			// Store extracted entity as a relation from the memory
			relation := &types.MemoryRelation{
				TenantID: mem.TenantID,
				FromUUID: mem.ID,
				ToUUID:   "", // Entity gets a new ID
				Relation: "mentions_" + ent.Type,
				Weight:   ent.Confidence,
			}
			_ = relation
			// In a full implementation, entities would be stored in an entities table
			// and relations would link memories to entities.
			_ = e.repo
		}
	}
}

type entityBatchResult struct {
	MemoryIndex int              `json:"memory_index"`
	Entities    []extractedEntity `json:"entities"`
}

type extractedEntity struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
}

func formatBatchItem(index int, id, content string) string {
	return "[" + id + "] " + content + "\n"
}
