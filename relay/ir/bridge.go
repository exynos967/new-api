package ir

import (
	"container/list"
	"sync"
	"time"
)

// ToolCallBridgeEntry preserves provider-specific correlation metadata that does not survive
// a client protocol that lacks equivalent fields (e.g., OpenAI Responses fc_* item IDs or
// Google Gemini thoughtSignature across OpenAI Chat / Anthropic turns).
type ToolCallBridgeEntry struct {
	ToolCallID       string
	ResponsesItemID  string
	ToolName         string
	Namespace        string
	ThoughtSignature string
	OriginFormat     Format
}

// BridgeState is the per-conversion serializable bag exported after responses and injected before requests.
type BridgeState struct {
	ToolCalls map[string]ToolCallBridgeEntry
}

// NewBridgeState allocates an empty BridgeState.
func NewBridgeState() *BridgeState {
	return &BridgeState{
		ToolCalls: make(map[string]ToolCallBridgeEntry),
	}
}

// RecordToolCall merges non-empty metadata fields for a tool call ID.
func (b *BridgeState) RecordToolCall(entry ToolCallBridgeEntry) {
	if b == nil || entry.ToolCallID == "" {
		return
	}
	if b.ToolCalls == nil {
		b.ToolCalls = make(map[string]ToolCallBridgeEntry)
	}
	existing := b.ToolCalls[entry.ToolCallID]
	existing.ToolCallID = entry.ToolCallID
	if entry.ResponsesItemID != "" {
		existing.ResponsesItemID = entry.ResponsesItemID
	}
	if entry.ToolName != "" {
		existing.ToolName = entry.ToolName
	}
	if entry.Namespace != "" {
		existing.Namespace = entry.Namespace
	}
	if entry.ThoughtSignature != "" {
		existing.ThoughtSignature = entry.ThoughtSignature
	}
	if entry.OriginFormat != FormatUnknown {
		existing.OriginFormat = entry.OriginFormat
	}
	b.ToolCalls[entry.ToolCallID] = existing
}

// ExtractResponseBridge captures cross-turn metadata from a decoded IRResponse.
func (b *BridgeState) ExtractResponseBridge(resp *Response, origin Format) {
	if b == nil || resp == nil {
		return
	}
	for _, ch := range resp.Choices {
		for _, part := range ch.Message.Parts {
			if part.Type == PartToolCall && part.ToolCall != nil && part.ToolCall.ID != "" {
				b.RecordToolCall(ToolCallBridgeEntry{
					ToolCallID:       part.ToolCall.ID,
					ResponsesItemID:  part.ToolCall.ItemID,
					ToolName:         part.ToolCall.Name,
					Namespace:        part.ToolCall.Namespace,
					ThoughtSignature: part.ToolCall.ThoughtSignature,
					OriginFormat:     origin,
				})
			}
		}
	}
}

// InjectIntoRequest enriches request tool calls / results with cached cross-turn metadata.
func (b *BridgeState) InjectIntoRequest(req *Request) {
	if b == nil || req == nil || len(b.ToolCalls) == 0 {
		return
	}
	for mi := range req.Messages {
		for pi := range req.Messages[mi].Parts {
			part := &req.Messages[mi].Parts[pi]
			switch part.Type {
			case PartToolCall:
				if part.ToolCall == nil || part.ToolCall.ID == "" {
					continue
				}
				entry, ok := b.ToolCalls[part.ToolCall.ID]
				if !ok {
					continue
				}
				if part.ToolCall.ItemID == "" && entry.ResponsesItemID != "" {
					part.ToolCall.ItemID = entry.ResponsesItemID
				}
				if part.ToolCall.Name == "" && entry.ToolName != "" {
					part.ToolCall.Name = entry.ToolName
				}
				if part.ToolCall.Namespace == "" && entry.Namespace != "" {
					part.ToolCall.Namespace = entry.Namespace
				}
				if part.ToolCall.ThoughtSignature == "" && entry.ThoughtSignature != "" {
					part.ToolCall.ThoughtSignature = entry.ThoughtSignature
				}
			case PartToolResult:
				if part.ToolResult == nil || part.ToolResult.ToolCallID == "" {
					continue
				}
				if part.ToolResult.Name == "" {
					if entry, ok := b.ToolCalls[part.ToolResult.ToolCallID]; ok && entry.ToolName != "" {
						part.ToolResult.Name = entry.ToolName
					}
				}
			}
		}
	}
}

type bridgeCacheRecord struct {
	key       string
	entry     ToolCallBridgeEntry
	expiresAt time.Time
}

// TurnBridge is a concurrency-safe TTL+LRU cache that bridges opaque cross-turn metadata
// across independent HTTP turns in the gateway.
type TurnBridge struct {
	mu      sync.Mutex
	ttl     time.Duration
	maxSize int
	now     func() time.Time
	lru     *list.List
	items   map[string]*list.Element
}

const (
	defaultTurnBridgeTTL     = 30 * time.Minute
	defaultTurnBridgeMaxSize = 10000
)

// NewTurnBridge creates a TurnBridge with the given TTL and maximum entries.
func NewTurnBridge(ttl time.Duration, maxSize int) *TurnBridge {
	if ttl <= 0 {
		ttl = defaultTurnBridgeTTL
	}
	if maxSize <= 0 {
		maxSize = defaultTurnBridgeMaxSize
	}
	return &TurnBridge{
		ttl:     ttl,
		maxSize: maxSize,
		now:     time.Now,
		lru:     list.New(),
		items:   make(map[string]*list.Element, 256),
	}
}

var defaultTurnBridge = NewTurnBridge(defaultTurnBridgeTTL, defaultTurnBridgeMaxSize)

// DefaultTurnBridge returns the process-wide TurnBridge instance used by the relay pipeline.
func DefaultTurnBridge() *TurnBridge {
	return defaultTurnBridge
}

func makeBridgeKey(scope, toolCallID string) string {
	if scope == "" {
		return toolCallID
	}
	return scope + "\x00" + toolCallID
}

// Save stores all tool-call entries from state under scope.
func (tb *TurnBridge) Save(scope string, state *BridgeState) {
	if tb == nil || state == nil || len(state.ToolCalls) == 0 {
		return
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := tb.now()
	exp := now.Add(tb.ttl)
	for id, entry := range state.ToolCalls {
		if id == "" {
			continue
		}
		if entry.ResponsesItemID == "" && entry.ThoughtSignature == "" && entry.Namespace == "" && entry.ToolName == "" {
			continue
		}
		k := makeBridgeKey(scope, id)
		if elem, ok := tb.items[k]; ok {
			rec := elem.Value.(*bridgeCacheRecord)
			rec.entry = mergeBridgeEntry(rec.entry, entry)
			rec.expiresAt = exp
			tb.lru.MoveToFront(elem)
			continue
		}
		rec := &bridgeCacheRecord{
			key:       k,
			entry:     entry,
			expiresAt: exp,
		}
		elem := tb.lru.PushFront(rec)
		tb.items[k] = elem
	}
	tb.evictLocked(now)
}

// LoadForRequest looks up cached entries for every tool call / tool result ID referenced in req.
// Entries are read with non-destructive Get semantics so multi-turn history replays remain intact.
func (tb *TurnBridge) LoadForRequest(scope string, req *Request) *BridgeState {
	state := NewBridgeState()
	if tb == nil || req == nil {
		return state
	}
	ids := collectRequestToolCallIDs(req)
	if len(ids) == 0 {
		return state
	}

	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := tb.now()
	for _, id := range ids {
		k := makeBridgeKey(scope, id)
		elem, ok := tb.items[k]
		if !ok && scope != "" {
			// Fallback to unscoped lookup when scope changed (e.g. retry across channels).
			elem, ok = tb.items[id]
		}
		if !ok {
			continue
		}
		rec := elem.Value.(*bridgeCacheRecord)
		if now.After(rec.expiresAt) {
			tb.lru.Remove(elem)
			delete(tb.items, rec.key)
			continue
		}
		tb.lru.MoveToFront(elem)
		state.ToolCalls[id] = rec.entry
	}
	return state
}

// Clear removes all cached bridge records.
func (tb *TurnBridge) Clear() {
	if tb == nil {
		return
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.lru.Init()
	tb.items = make(map[string]*list.Element, 64)
}

// Len returns the current number of cached entries (for observability/testing).
func (tb *TurnBridge) Len() int {
	if tb == nil {
		return 0
	}
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return len(tb.items)
}

func (tb *TurnBridge) evictLocked(now time.Time) {
	// Evict expired items from tail first, then enforce maxSize.
	for elem := tb.lru.Back(); elem != nil; {
		prev := elem.Prev()
		rec := elem.Value.(*bridgeCacheRecord)
		if now.After(rec.expiresAt) || len(tb.items) > tb.maxSize {
			tb.lru.Remove(elem)
			delete(tb.items, rec.key)
		} else {
			break
		}
		elem = prev
	}
}

func mergeBridgeEntry(base, next ToolCallBridgeEntry) ToolCallBridgeEntry {
	if next.ToolCallID != "" {
		base.ToolCallID = next.ToolCallID
	}
	if next.ResponsesItemID != "" {
		base.ResponsesItemID = next.ResponsesItemID
	}
	if next.ToolName != "" {
		base.ToolName = next.ToolName
	}
	if next.Namespace != "" {
		base.Namespace = next.Namespace
	}
	if next.ThoughtSignature != "" {
		base.ThoughtSignature = next.ThoughtSignature
	}
	if next.OriginFormat != FormatUnknown {
		base.OriginFormat = next.OriginFormat
	}
	return base
}

func collectRequestToolCallIDs(req *Request) []string {
	var ids []string
	seen := make(map[string]struct{})
	for _, msg := range req.Messages {
		for _, part := range msg.Parts {
			var id string
			if part.Type == PartToolCall && part.ToolCall != nil {
				id = part.ToolCall.ID
			} else if part.Type == PartToolResult && part.ToolResult != nil {
				id = part.ToolResult.ToolCallID
			}
			if id == "" {
				continue
			}
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	return ids
}
