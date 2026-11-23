package google

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/relay/ir"
)

// DecodeStreamEvent parses one Google GenAI streamGenerateContent SSE JSON chunk into normalized IR StreamEvents.
func (c *Codec) DecodeStreamEvent(_ string, rawData []byte, ctx *ir.StreamContext) ([]ir.StreamEvent, error) {
	trimmed := bytes.TrimSpace(rawData)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("[DONE]")) {
		return nil, nil
	}
	if ctx == nil {
		ctx = ir.NewStreamContext(ir.FormatGoogleGenAI, ir.FormatGoogleGenAI)
	}

	var gResp googleResponse
	if err := json.Unmarshal(trimmed, &gResp); err != nil {
		return nil, fmt.Errorf("google: decode stream chunk: %w", err)
	}

	if gResp.Error != nil {
		codeStr := ""
		if gResp.Error.Code != 0 {
			codeStr = strconv.Itoa(gResp.Error.Code)
		}
		return []ir.StreamEvent{
			{
				Type:         ir.EventError,
				ErrorMessage: gResp.Error.Message,
				ErrorType:    gResp.Error.Status,
				ErrorCode:    codeStr,
			},
		}, nil
	}

	if gResp.ResponseID != "" {
		ctx.ResponseID = gResp.ResponseID
	}
	if gResp.ModelVersion != "" {
		ctx.Model = gResp.ModelVersion
	}

	var events []ir.StreamEvent
	if !ctx.Started {
		ctx.Started = true
		events = append(events, ir.StreamEvent{
			Type:       ir.EventStreamStart,
			ResponseID: ctx.ResponseID,
			Model:      ctx.Model,
			Created:    ctx.Created,
		})
	}

	for ci, cand := range gResp.Candidates {
		choiceIdx := cand.Index
		if ci > 0 && choiceIdx == 0 {
			choiceIdx = ci
		}

		if cand.Content != nil {
			for _, p := range cand.Content.Parts {
				switch {
				case p.Thought:
					if p.Text != nil && *p.Text != "" {
						events = append(events, ir.StreamEvent{
							Type:         ir.EventReasoningDelta,
							ChoiceIndex:  choiceIdx,
							Reasoning:    *p.Text,
							OriginFormat: ir.FormatGoogleGenAI,
						})
					}
					if p.ThoughtSignature != "" {
						events = append(events, ir.StreamEvent{
							Type:         ir.EventReasoningSignatureDelta,
							ChoiceIndex:  choiceIdx,
							Signature:    p.ThoughtSignature,
							OriginFormat: ir.FormatGoogleGenAI,
						})
					}

				case p.FunctionCall != nil:
					toolIdx := ctx.GoogleSeenToolCallCount
					ctx.GoogleSeenToolCallCount++
					id := p.FunctionCall.ID
					if id == "" {
						id = fmt.Sprintf("call_%d", toolIdx)
					}
					args := normalizeJSONArgs(p.FunctionCall.Args)
					tc, _ := ctx.RegisterToolCall(toolIdx, id, "", p.FunctionCall.Name, "function", p.ThoughtSignature)
					// Ensure BridgeState records Google origin accurately even when TargetFormat is another dialect.
					if ctx.BridgeState != nil && tc.ID != "" {
						ctx.BridgeState.RecordToolCall(ir.ToolCallBridgeEntry{
							ToolCallID:       tc.ID,
							ToolName:         tc.Name,
							ThoughtSignature: tc.ThoughtSignature,
							OriginFormat:     ir.FormatGoogleGenAI,
						})
					}
					tc.Args.Reset()
					tc.Args.WriteString(args)
					tc.Started = true
					tc.Closed = true

					blockIdx := ctx.NextBlockIndex
					ctx.NextBlockIndex++
					events = append(events,
						ir.StreamEvent{
							Type:             ir.EventToolCallStart,
							ChoiceIndex:      choiceIdx,
							BlockIndex:       blockIdx,
							BlockType:        ir.PartToolCall,
							ToolCallIndex:    tc.Index,
							ToolCallID:       tc.ID,
							ToolName:         tc.Name,
							ToolKind:         "function",
							ThoughtSignature: tc.ThoughtSignature,
							OriginFormat:     ir.FormatGoogleGenAI,
						},
						ir.StreamEvent{
							Type:          ir.EventToolCallDelta,
							ChoiceIndex:   choiceIdx,
							BlockIndex:    blockIdx,
							ToolCallIndex: tc.Index,
							ToolCallID:    tc.ID,
							ToolName:      tc.Name,
							ToolArguments: args,
						},
						ir.StreamEvent{
							Type:          ir.EventContentBlockEnd,
							ChoiceIndex:   choiceIdx,
							BlockIndex:    blockIdx,
							BlockType:     ir.PartToolCall,
							ToolCallIndex: tc.Index,
							ToolCallID:    tc.ID,
						},
					)

				case p.InlineData != nil && p.InlineData.Data != "":
					mime := p.InlineData.MimeType
					var text string
					if mime == "" || strings.HasPrefix(strings.ToLower(mime), "image/") {
						if mime == "" {
							mime = "image/png"
						}
						text = fmt.Sprintf("![image](data:%s;base64,%s)", mime, p.InlineData.Data)
					} else {
						text = fmt.Sprintf("[media](data:%s;base64,%s)", mime, p.InlineData.Data)
					}
					events = append(events, ir.StreamEvent{
						Type:        ir.EventTextDelta,
						ChoiceIndex: choiceIdx,
						Text:        text,
					})

				case p.ExecutableCode != nil:
					events = append(events, ir.StreamEvent{
						Type:        ir.EventTextDelta,
						ChoiceIndex: choiceIdx,
						Text:        formatExecutableCodeText(p.ExecutableCode),
					})

				case p.CodeExecutionResult != nil:
					events = append(events, ir.StreamEvent{
						Type:        ir.EventTextDelta,
						ChoiceIndex: choiceIdx,
						Text:        formatCodeExecutionResultText(p.CodeExecutionResult),
					})

				case p.Text != nil && *p.Text != "":
					events = append(events, ir.StreamEvent{
						Type:        ir.EventTextDelta,
						ChoiceIndex: choiceIdx,
						Text:        *p.Text,
					})

				case p.ThoughtSignature != "":
					events = append(events, ir.StreamEvent{
						Type:         ir.EventReasoningSignatureDelta,
						ChoiceIndex:  choiceIdx,
						Signature:    p.ThoughtSignature,
						OriginFormat: ir.FormatGoogleGenAI,
					})
				}
			}
		}
	}

	if gResp.UsageMetadata != nil {
		u := decodeUsageMetadata(gResp.UsageMetadata)
		ctx.MergeUsage(u)
		events = append(events, ir.StreamEvent{
			Type:  ir.EventUsage,
			Usage: u,
		})
	}

	for ci, cand := range gResp.Candidates {
		if cand.FinishReason == "" {
			continue
		}
		choiceIdx := cand.Index
		if ci > 0 && choiceIdx == 0 {
			choiceIdx = ci
		}
		hasToolCalls := ctx.GoogleSeenToolCallCount > 0 || len(ctx.ToolCallOrder) > 0
		events = append(events, ir.StreamEvent{
			Type:            ir.EventFinish,
			ChoiceIndex:     choiceIdx,
			FinishReason:    mapGoogleFinishReason(cand.FinishReason, hasToolCalls),
			RawFinishReason: cand.FinishReason,
		})
	}

	if len(gResp.Candidates) == 0 && gResp.PromptFeedback != nil && gResp.PromptFeedback.BlockReason != "" {
		events = append(events, ir.StreamEvent{
			Type:            ir.EventFinish,
			ChoiceIndex:     0,
			FinishReason:    ir.FinishContentFilter,
			RawFinishReason: gResp.PromptFeedback.BlockReason,
		})
	}

	return events, nil
}

// EncodeStreamEvent translates a single IR StreamEvent into zero or more Google GenAI SSE frames.
// Tool calls are buffered until complete so each Gemini functionCall part contains valid full args.
func (c *Codec) EncodeStreamEvent(ev ir.StreamEvent, ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	if ctx == nil {
		ctx = ir.NewStreamContext(ir.FormatGoogleGenAI, ir.FormatGoogleGenAI)
	}
	if ev.ResponseID != "" {
		ctx.ResponseID = ev.ResponseID
	}
	if ev.Model != "" {
		ctx.Model = ev.Model
	}
	if ev.Created > 0 && ctx.Created == 0 {
		ctx.Created = ev.Created
	}

	switch ev.Type {
	case ir.EventStreamStart:
		ctx.Started = true
		return nil, nil

	case ir.EventContentBlockStart:
		ctx.Started = true
		if ev.BlockType != "" && ev.BlockType != ir.PartToolCall {
			return flushAllPendingToolCalls(ctx)
		}
		return nil, nil

	case ir.EventTextDelta:
		ctx.Started = true
		frames, err := flushAllPendingToolCalls(ctx)
		if err != nil {
			return nil, err
		}
		if ev.Text == "" {
			return frames, nil
		}
		frame, err := buildStreamChunkFrame(ctx, ev.ChoiceIndex, []googlePart{{Text: strPtr(ev.Text)}}, "", nil)
		if err != nil {
			return frames, err
		}
		return append(frames, frame), nil

	case ir.EventRefusalDelta:
		ctx.Started = true
		frames, err := flushAllPendingToolCalls(ctx)
		if err != nil {
			return nil, err
		}
		if ev.Refusal == "" {
			return frames, nil
		}
		frame, err := buildStreamChunkFrame(ctx, ev.ChoiceIndex, []googlePart{{Text: strPtr(ev.Refusal)}}, "", nil)
		if err != nil {
			return frames, err
		}
		return append(frames, frame), nil

	case ir.EventReasoningDelta:
		ctx.Started = true
		frames, err := flushAllPendingToolCalls(ctx)
		if err != nil {
			return nil, err
		}
		if ev.Reasoning == "" {
			return frames, nil
		}
		frame, err := buildStreamChunkFrame(ctx, ev.ChoiceIndex, []googlePart{{
			Text:    strPtr(ev.Reasoning),
			Thought: true,
		}}, "", nil)
		if err != nil {
			return frames, err
		}
		return append(frames, frame), nil

	case ir.EventReasoningSignatureDelta:
		ctx.Started = true
		if ev.Signature == "" || !shouldEmitGoogleSignature(ev.OriginFormat, ctx) {
			return nil, nil
		}
		frames, err := flushAllPendingToolCalls(ctx)
		if err != nil {
			return nil, err
		}
		frame, err := buildStreamChunkFrame(ctx, ev.ChoiceIndex, []googlePart{{
			Text:             strPtr(""),
			Thought:          true,
			ThoughtSignature: ev.Signature,
		}}, "", nil)
		if err != nil {
			return frames, err
		}
		return append(frames, frame), nil

	case ir.EventToolCallStart:
		ctx.Started = true
		sig := ""
		if ev.ThoughtSignature != "" && shouldEmitGoogleSignature(ev.OriginFormat, ctx) {
			sig = ev.ThoughtSignature
		}
		idx := ev.ToolCallIndex
		if ev.ToolCallID != "" {
			if existing := ctx.ToolCallsByID[ev.ToolCallID]; existing != nil {
				idx = existing.Index
			} else if existingAtIdx := ctx.ToolCallsByIndex[idx]; existingAtIdx != nil && existingAtIdx.ID != ev.ToolCallID {
				idx = -1
			}
		}
		tc, _ := ctx.RegisterToolCall(idx, ev.ToolCallID, ev.ToolCallItemID, ev.ToolName, ev.ToolKind, sig)
		if tc.BlockIndex < 0 || ev.BlockIndex > 0 {
			tc.BlockIndex = ev.BlockIndex
		}
		tc.Started = true
		if ev.ChoiceIndex > 0 {
			if tc.ProviderMetadata == nil {
				tc.ProviderMetadata = make(map[string]any)
			}
			tc.ProviderMetadata["choice_index"] = ev.ChoiceIndex
		}
		if ev.ToolArguments != "" {
			tc.Args.WriteString(ev.ToolArguments)
		}
		return nil, nil

	case ir.EventToolCallDelta:
		ctx.Started = true
		tc := findTrackedToolCall(ctx, ev)
		if tc == nil {
			sig := ""
			if ev.ThoughtSignature != "" && shouldEmitGoogleSignature(ev.OriginFormat, ctx) {
				sig = ev.ThoughtSignature
			}
			tc, _ = ctx.RegisterToolCall(ev.ToolCallIndex, ev.ToolCallID, ev.ToolCallItemID, ev.ToolName, ev.ToolKind, sig)
			tc.BlockIndex = ev.BlockIndex
			tc.Started = true
		} else {
			if ev.ToolName != "" && tc.Name == "" {
				tc.Name = ev.ToolName
			}
			if ev.ThoughtSignature != "" && tc.ThoughtSignature == "" && shouldEmitGoogleSignature(ev.OriginFormat, ctx) {
				tc.ThoughtSignature = ev.ThoughtSignature
			}
		}
		if ev.ToolArguments != "" {
			tc.Args.WriteString(ev.ToolArguments)
		}
		return nil, nil

	case ir.EventContentBlockEnd:
		ctx.Started = true
		tc := matchToolCallForBlockEnd(ctx, ev)
		if tc == nil || tc.Closed {
			return nil, nil
		}
		frame, err := flushSingleToolCall(ctx, tc)
		if err != nil {
			return nil, err
		}
		return []ir.SSEFrame{frame}, nil

	case ir.EventUsage:
		if ev.Usage != nil {
			ctx.MergeUsage(ev.Usage)
		}
		return nil, nil

	case ir.EventFinish:
		ctx.Started = true
		frames, err := flushAllPendingToolCalls(ctx)
		if err != nil {
			return nil, err
		}
		fr := ev.FinishReason
		ctx.PendingFinish = &fr
		ctx.PendingRawFinish = ev.RawFinishReason
		if ev.Usage != nil {
			ctx.MergeUsage(ev.Usage)
		}
		return frames, nil

	case ir.EventStreamEnd:
		return c.FlushStream(ctx)

	case ir.EventError:
		frames, err := flushAllPendingToolCalls(ctx)
		if err != nil {
			return nil, err
		}
		code := 500
		if parsed, parseErr := strconv.Atoi(strings.TrimSpace(ev.ErrorCode)); parseErr == nil && parsed > 0 {
			code = parsed
		}
		status := ev.ErrorType
		if status == "" {
			status = "INTERNAL"
		}
		b, err := json.Marshal(googleResponse{
			Error: &googleError{
				Code:    code,
				Message: ev.ErrorMessage,
				Status:  status,
			},
		})
		if err != nil {
			return frames, err
		}
		ctx.FinishEmitted = true
		return append(frames, ir.SSEFrame{Data: b}), nil

	case ir.EventPassthrough:
		if ctx.ShouldPreserveMetadata(ev.OriginFormat) && !isNullOrEmptyRaw(ev.PassthroughData) {
			return []ir.SSEFrame{{Data: bytes.Clone(ev.PassthroughData)}}, nil
		}
		return nil, nil

	default:
		return nil, nil
	}
}

// FlushStream flushes any buffered tool calls and emits the final Google GenAI SSE chunk
// with finishReason and usageMetadata (without an SSE event header or [DONE] sentinel).
func (c *Codec) FlushStream(ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	if ctx == nil {
		return nil, nil
	}

	frames, err := flushAllPendingToolCalls(ctx)
	if err != nil {
		return nil, err
	}

	if !ctx.FinishEmitted && (ctx.PendingFinish != nil || ctx.LastUsage != nil || ctx.Started || len(frames) > 0) {
		finishReason := "STOP"
		if ctx.PendingFinish != nil {
			if mapped := encodeGoogleFinishReason(*ctx.PendingFinish, ctx.PendingRawFinish); mapped != "" {
				finishReason = mapped
			}
		}
		finalFrame, err := buildStreamChunkFrame(
			ctx,
			0,
			[]googlePart{},
			finishReason,
			encodeUsageMetadata(ctx.LastUsage),
		)
		if err != nil {
			return frames, err
		}
		frames = append(frames, finalFrame)
		ctx.FinishEmitted = true
		if ctx.LastUsage != nil {
			ctx.UsageEmitted = true
		}
		ctx.PendingFinish = nil
		ctx.PendingUsage = nil
	}

	return frames, nil
}

func shouldEmitGoogleSignature(origin ir.Format, ctx *ir.StreamContext) bool {
	if origin == ir.FormatGoogleGenAI {
		return true
	}
	if origin != ir.FormatUnknown {
		return false
	}
	return ctx != nil && (ctx.SourceFormat == ir.FormatGoogleGenAI || ctx.MetadataMode == ir.MetadataPreserve)
}

func findTrackedToolCall(ctx *ir.StreamContext, ev ir.StreamEvent) *ir.TrackedToolCall {
	if ev.ToolCallID != "" {
		if tc := ctx.ToolCallsByID[ev.ToolCallID]; tc != nil {
			return tc
		}
	}
	if ev.ToolCallIndex > 0 {
		if tc := ctx.ToolCallsByIndex[ev.ToolCallIndex]; tc != nil && !tc.Closed {
			return tc
		}
	}
	if ev.BlockIndex > 0 {
		for _, tc := range ctx.ToolCallOrder {
			if !tc.Closed && tc.BlockIndex == ev.BlockIndex {
				return tc
			}
		}
	}
	if ev.ToolCallIndex >= 0 {
		if tc := ctx.ToolCallsByIndex[ev.ToolCallIndex]; tc != nil && !tc.Closed {
			return tc
		}
	}
	if ev.BlockIndex >= 0 {
		for _, tc := range ctx.ToolCallOrder {
			if !tc.Closed && tc.BlockIndex == ev.BlockIndex {
				return tc
			}
		}
	}
	for i := len(ctx.ToolCallOrder) - 1; i >= 0; i-- {
		if !ctx.ToolCallOrder[i].Closed {
			return ctx.ToolCallOrder[i]
		}
	}
	return nil
}

func matchToolCallForBlockEnd(ctx *ir.StreamContext, ev ir.StreamEvent) *ir.TrackedToolCall {
	if ev.ToolCallID != "" {
		if tc := ctx.ToolCallsByID[ev.ToolCallID]; tc != nil && !tc.Closed {
			return tc
		}
	}
	if ev.BlockType != "" && ev.BlockType != ir.PartToolCall {
		return nil
	}
	if ev.BlockIndex >= 0 {
		for _, tc := range ctx.ToolCallOrder {
			if !tc.Closed && tc.BlockIndex == ev.BlockIndex {
				return tc
			}
		}
	}
	if ev.BlockType == ir.PartToolCall {
		if ev.ToolCallIndex >= 0 {
			if tc := ctx.ToolCallsByIndex[ev.ToolCallIndex]; tc != nil && !tc.Closed {
				return tc
			}
		}
		for _, tc := range ctx.ToolCallOrder {
			if !tc.Closed {
				return tc
			}
		}
	}
	return nil
}

func flushSingleToolCall(ctx *ir.StreamContext, tc *ir.TrackedToolCall) (ir.SSEFrame, error) {
	tc.Closed = true
	choiceIdx := 0
	if tc.ProviderMetadata != nil {
		if v, ok := tc.ProviderMetadata["choice_index"].(int); ok {
			choiceIdx = v
		}
	}
	part := googlePart{
		FunctionCall: &googleFunctionCall{
			ID:   tc.ID,
			Name: tc.Name,
			Args: parseToolArgsToObject(tc.Args.String()),
		},
		ThoughtSignature: tc.ThoughtSignature,
	}
	return buildStreamChunkFrame(ctx, choiceIdx, []googlePart{part}, "", nil)
}

func flushAllPendingToolCalls(ctx *ir.StreamContext) ([]ir.SSEFrame, error) {
	var frames []ir.SSEFrame
	for _, tc := range ctx.ToolCallOrder {
		if tc.Closed {
			continue
		}
		frame, err := flushSingleToolCall(ctx, tc)
		if err != nil {
			return frames, err
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

func buildStreamChunkFrame(ctx *ir.StreamContext, choiceIdx int, parts []googlePart, finishReason string, usage *googleUsageMetadata) (ir.SSEFrame, error) {
	if parts == nil {
		parts = []googlePart{}
	}
	chunk := googleResponse{
		ResponseID:   ctx.ResponseID,
		ModelVersion: ctx.Model,
		Candidates: []googleCandidate{
			{
				Index: choiceIdx,
				Content: &googleContent{
					Role:  "model",
					Parts: parts,
				},
				FinishReason: finishReason,
			},
		},
		UsageMetadata: usage,
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return ir.SSEFrame{}, err
	}
	return ir.SSEFrame{Data: data}, nil
}
