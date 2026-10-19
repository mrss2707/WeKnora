package workers

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// EntityExtractor buffers memories and periodically extracts entities via LLM.
// With a resolver each (tenant, knowledge base) group uses its own chat model.
type EntityExtractor struct {
	repo      interfaces.MemoryRepositoryV2
	chat      chat.Chat
	resolver  ModelResolver
	patcher   interfaces.MemoryV2Patcher
	mu        sync.Mutex
	buffer    []*types.AgentMemory
	ticker    *time.Ticker
	batchSize int
	// callTimeout bounds one model call (extractorModelTimeout when zero).
	callTimeout time.Duration
}

func (e *EntityExtractor) modelTimeout() time.Duration {
	if e.callTimeout > 0 {
		return e.callTimeout
	}
	return extractorModelTimeout
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

// WithResolver makes the extractor resolve each scope's chat model.
func (e *EntityExtractor) WithResolver(r ModelResolver) *EntityExtractor {
	e.resolver = r
	return e
}

func (e *EntityExtractor) WithPatcher(p interfaces.MemoryV2Patcher) *EntityExtractor {
	e.patcher = p
	return e
}

// Enqueue adds a memory to the extraction queue.
func (e *EntityExtractor) Enqueue(memory *types.AgentMemory) {
	if memory == nil || memory.Verdict.IsProtected() {
		return
	}
	snapshot := *memory
	snapshot.Tags = append(types.TagsArray(nil), memory.Tags...)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.buffer = append(e.buffer, &snapshot)

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

	if e.resolver == nil {
		e.processBatch(ctx, batch)
		return
	}
	// Never send memories of different tenants/knowledge bases in one prompt.
	groups := map[Scope][]*types.AgentMemory{}
	var order []Scope
	for _, m := range batch {
		sc := Scope{TenantID: m.TenantID, KbID: m.KbID}
		if _, ok := groups[sc]; !ok {
			order = append(order, sc)
		}
		groups[sc] = append(groups[sc], m)
	}
	for _, sc := range order {
		ch, err := chatForPurpose(ctx, e.resolver, sc, types.MemoryModelPurposeText)
		if err != nil {
			logger.Errorf(ctx, "entity extractor: resolve chat model for %s/%s: %v", sc.TenantID, sc.KbID, err)
			continue
		}
		e.processBatchWith(ctx, ch, groups[sc])
	}
}

// processBatch sends a batch to the LLM and stores extracted entities.
func (e *EntityExtractor) processBatch(ctx context.Context, batch []*types.AgentMemory) {
	e.processBatchWith(ctx, e.chat, batch)
}

// processBatchWith runs extraction for one batch with the given chat model.
func (e *EntityExtractor) processBatchWith(ctx context.Context, chatModel chat.Chat, batch []*types.AgentMemory) {
	if chatModel == nil {
		return
	}
	ctx = context.WithValue(ctx, types.ActorKey{}, "system")

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

	callCtx, cancelCall := context.WithTimeout(ctx, e.modelTimeout())
	resp, err := chatModel.Chat(callCtx, []chat.Message{
		{Role: "user", Content: prompt},
	}, nil)
	cancelCall()
	if err != nil {
		logger.Errorf(ctx, "entity extraction batch LLM call failed: %v", err)
		return
	}

	var extracted []entityBatchResult
	if err := json.Unmarshal([]byte(entityJSON(resp.Content)), &extracted); err != nil {
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
			// Append entity names to memory tags, avoiding duplicates
			found := false
			for _, tag := range mem.Tags {
				if strings.EqualFold(tag, ent.Name) {
					found = true
					break
				}
			}
			if !found {
				mem.Tags = append(mem.Tags, ent.Name)
			}
		}
		// Persist only tags, with the version the model reviewed, through
		// the audited service path when wired by the production container.
		var err error
		if e.patcher != nil {
			tags := []string(mem.Tags)
			_, err = e.patcher.PatchMemory(ctx, mem.TenantID, mem.ID, interfaces.MemoryV2Patch{Tags: &tags, ExpectedUpdatedAt: &mem.UpdatedAt})
		} else {
			err = e.repo.Update(ctx, mem)
		}
		if err != nil {
			logger.Errorf(ctx, "entity extractor: failed to update memory tags for %s: %v", mem.ID, err)
		}
	}
}

type entityBatchResult struct {
	MemoryIndex int               `json:"memory_index"`
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

var jsonArray = regexp.MustCompile(`(?s)\[.*\]`)

// entityJSON extracts the JSON array from a reply, tolerating code fences and
// surrounding prose that hosted models often add.
func entityJSON(reply string) string {
	if raw := jsonArray.FindString(reply); raw != "" {
		return raw
	}
	return reply
}
