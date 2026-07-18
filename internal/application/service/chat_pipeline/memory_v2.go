package chatpipeline

import (
	"context"
	"fmt"
	"sync"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type MemoryPluginV2 struct {
	memoryService interfaces.MemoryServiceV2
}

func NewMemoryPluginV2(eventManager *EventManager, memoryService interfaces.MemoryServiceV2) *MemoryPluginV2 {
	res := &MemoryPluginV2{
		memoryService: memoryService,
	}
	eventManager.Register(res)
	return res
}

func (p *MemoryPluginV2) ActivationEvents() []types.EventType {
	return []types.EventType{
		types.MEMORY_RETRIEVAL,
		types.MEMORY_STORAGE,
	}
}

func (p *MemoryPluginV2) OnEvent(
	ctx context.Context,
	eventType types.EventType,
	chatManage *types.ChatManage,
	next func() *PluginError,
) *PluginError {
	switch eventType {
	case types.MEMORY_RETRIEVAL:
		return p.handleRetrieval(ctx, chatManage, next)
	case types.MEMORY_STORAGE:
		return p.handleStorage(ctx, chatManage, next)
	default:
		return next()
	}
}

func (p *MemoryPluginV2) handleRetrieval(
	ctx context.Context,
	chatManage *types.ChatManage,
	next func() *PluginError,
) *PluginError {
	if !chatManage.EnableMemory {
		return next()
	}
	logger.Info(ctx, "Start to retrieve memory (V2)")

	query := chatManage.RewriteQuery
	if query == "" {
		query = chatManage.Query
	}

	memoryContext, err := p.memoryService.RetrieveMemory(ctx, chatManage.UserID, query)
	if err != nil {
		logger.Errorf(ctx, "failed to retrieve memory (V2): %v", err)
		return next()
	}

	if len(memoryContext.RelatedEpisodes) > 0 {
		memoryStr := "\n\nRelevant Memory:\n"
		for _, ep := range memoryContext.RelatedEpisodes {
			memoryStr += fmt.Sprintf("- %s (Summary: %s)\n", ep.CreatedAt.Format("2006-01-02"), ep.Summary)
		}
		chatManage.UserContent += memoryStr
		logger.Infof(ctx, "Retrieved memory (V2): %s", memoryStr)
	}
	logger.Info(ctx, "End to retrieve memory (V2)")

	return next()
}

func (p *MemoryPluginV2) handleStorage(
	ctx context.Context,
	chatManage *types.ChatManage,
	next func() *PluginError,
) *PluginError {
	if err := next(); err != nil {
		return err
	}

	if !chatManage.EnableMemory {
		return nil
	}

	logger.Info(ctx, "Start to store memory (V2)")
	if chatManage.ChatResponse != nil {
		messages := []types.Message{
			{Role: "user", Content: chatManage.Query},
			{Role: "assistant", Content: chatManage.ChatResponse.Content},
		}
		userID := chatManage.UserID
		sessionID := chatManage.SessionID
		bgCtx := context.WithoutCancel(ctx)
		go func() {
			if err := p.memoryService.AddEpisode(bgCtx, userID, sessionID, messages); err != nil {
				logger.Errorf(bgCtx, "failed to add episode (V2): %v", err)
			}
		}()
		return nil
	}

	if chatManage.EventBus != nil {
		var fullResponse string
		var storeOnce sync.Once
		userID := chatManage.UserID
		sessionID := chatManage.SessionID
		bgCtx := context.WithoutCancel(ctx)

		chatManage.EventBus.On(types.EventType(event.EventAgentFinalAnswer), func(_ context.Context, evt types.Event) error {
			data, ok := evt.Data.(event.AgentFinalAnswerData)
			if !ok {
				return nil
			}
			fullResponse += data.Content
			if data.Done {
				storeOnce.Do(func() {
					messages := []types.Message{
						{Role: "user", Content: chatManage.Query},
						{Role: "assistant", Content: fullResponse},
					}
					go func() {
						if err := p.memoryService.AddEpisode(bgCtx, userID, sessionID, messages); err != nil {
							logger.Errorf(bgCtx, "failed to add episode (V2): %v", err)
						}
					}()
				})
			}
			return nil
		})
	}
	logger.Info(ctx, "End to store memory (V2)")

	return nil
}
