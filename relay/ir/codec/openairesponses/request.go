package openairesponses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/relay/ir"
)

type responsesRequestWire struct {
	Model                string                  `json:"model"`
	Instructions         string                  `json:"instructions,omitempty"`
	Input                json.RawMessage         `json:"input,omitempty"`
	Tools                []json.RawMessage       `json:"tools,omitempty"`
	ToolChoice           json.RawMessage         `json:"tool_choice,omitempty"`
	ParallelToolCalls    *bool                   `json:"parallel_tool_calls,omitempty"`
	MaxOutputTokens      *int                    `json:"max_output_tokens,omitempty"`
	Temperature          *float64                `json:"temperature,omitempty"`
	TopP                 *float64                `json:"top_p,omitempty"`
	User                 string                  `json:"user,omitempty"`
	Store                *bool                   `json:"store,omitempty"`
	Truncation           string                  `json:"truncation,omitempty"`
	PreviousResponseID   string                  `json:"previous_response_id,omitempty"`
	ServiceTier          string                  `json:"service_tier,omitempty"`
	Stream               bool                    `json:"stream,omitempty"`
	Include              []string                `json:"include,omitempty"`
	Metadata             map[string]any          `json:"metadata,omitempty"`
	PromptCacheKey       string                  `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention string                  `json:"prompt_cache_retention,omitempty"`
	Reasoning            *responsesReasoningCfg  `json:"reasoning,omitempty"`
	Text                 *responsesTextConfig    `json:"text,omitempty"`
}

type responsesReasoningCfg struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type responsesTextConfig struct {
	Format    *responsesTextFormat `json:"format,omitempty"`
	Verbosity string               `json:"verbosity,omitempty"`
}

type responsesTextFormat struct {
	Type        string          `json:"type,omitempty"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type responsesInputItemWire struct {
	Type             string                     `json:"type,omitempty"`
	ID               string                     `json:"id,omitempty"`
	Status           string                     `json:"status,omitempty"`
	Role             string                     `json:"role,omitempty"`
	Content          json.RawMessage            `json:"content,omitempty"`
	CallID           string                     `json:"call_id,omitempty"`
	Name             string                     `json:"name,omitempty"`
	Arguments        string                     `json:"arguments,omitempty"`
	Input            string                     `json:"input,omitempty"`
	Output           json.RawMessage            `json:"output,omitempty"`
	Summary          []responsesReasoningPart   `json:"summary,omitempty"`
	EncryptedContent string                     `json:"encrypted_content,omitempty"`
}

type responsesReasoningPart struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text,omitempty"`
}

type responsesContentPartWire struct {
	Type        string                     `json:"type,omitempty"`
	Text        string                     `json:"text,omitempty"`
	Refusal     string                     `json:"refusal,omitempty"`
	ImageURL    json.RawMessage            `json:"image_url,omitempty"`
	Detail      string                     `json:"detail,omitempty"`
	FileID      string                     `json:"file_id,omitempty"`
	Filename    string                     `json:"filename,omitempty"`
	FileURL     string                     `json:"file_url,omitempty"`
	FileData    string                     `json:"file_data,omitempty"`
	InputAudio  *responsesInputAudioWire   `json:"input_audio,omitempty"`
	Annotations []responsesAnnotationWire  `json:"annotations,omitempty"`
}

type responsesInputAudioWire struct {
	Data   string `json:"data,omitempty"`
	Format string `json:"format,omitempty"`
}

type responsesAnnotationWire struct {
	Type       string `json:"type,omitempty"`
	URL        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	Text       string `json:"text,omitempty"`
	CitedText  string `json:"cited_text,omitempty"`
	StartIndex int    `json:"start_index,omitempty"`
	EndIndex   int    `json:"end_index,omitempty"`
}

type responsesToolWire struct {
	Type        string          `json:"type,omitempty"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
	Function    *struct {
		Name        string          `json:"name,omitempty"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
		Strict      *bool           `json:"strict,omitempty"`
	} `json:"function,omitempty"`
}

type responsesToolChoiceObj struct {
	Type  string `json:"type,omitempty"`
	Name  string `json:"name,omitempty"`
	Mode  string `json:"mode,omitempty"`
	Tools []struct {
		Type string `json:"type,omitempty"`
		Name string `json:"name,omitempty"`
	} `json:"tools,omitempty"`
}

// DecodeRequest parses an OpenAI Responses API JSON request into the provider-neutral IR.
func (c *Codec) DecodeRequest(raw []byte, ctx *ir.ConversionContext) (*ir.Request, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("openairesponses: empty request body")
	}
	var wire responsesRequestWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("openairesponses: decode request: %w", err)
	}

	req := &ir.Request{
		Model:             wire.Model,
		ParallelToolCalls: wire.ParallelToolCalls,
		Stream:            wire.Stream,
		Include:           wire.Include,
		Metadata:          wire.Metadata,
		Generation: ir.GenerationConfig{
			MaxTokens:            wire.MaxOutputTokens,
			Temperature:          wire.Temperature,
			TopP:                 wire.TopP,
			User:                 wire.User,
			Store:                wire.Store,
			Truncation:           wire.Truncation,
			PreviousResponseID:   wire.PreviousResponseID,
			ServiceTier:          wire.ServiceTier,
			PromptCacheKey:       wire.PromptCacheKey,
			PromptCacheRetention: wire.PromptCacheRetention,
		},
	}

	if wire.Instructions != "" {
		req.System = append(req.System, ir.ContentPart{
			Type: ir.PartText,
			Text: wire.Instructions,
		})
	}

	if wire.Reasoning != nil {
		rc := &ir.ReasoningConfig{
			Effort:  wire.Reasoning.Effort,
			Summary: wire.Reasoning.Summary,
		}
		if wire.Reasoning.Effort == "none" {
			enabled := false
			rc.Enabled = &enabled
			rc.Mode = "disabled"
		} else if wire.Reasoning.Effort != "" {
			enabled := true
			rc.Enabled = &enabled
		}
		req.Reasoning = rc
	}

	if wire.Text != nil {
		if wire.Text.Verbosity != "" {
			req.Generation.Verbosity = wire.Text.Verbosity
		}
		if wire.Text.Format != nil && wire.Text.Format.Type != "" {
			req.ResponseFormat = &ir.ResponseFormat{
				Type:        wire.Text.Format.Type,
				Name:        wire.Text.Format.Name,
				Description: wire.Text.Format.Description,
				Schema:      cloneRaw(wire.Text.Format.Schema),
				Strict:      wire.Text.Format.Strict,
			}
		}
	}

	if err := decodeRequestInput(wire.Input, req); err != nil {
		return nil, err
	}

	if len(wire.Tools) > 0 {
		req.Tools = decodeRequestTools(wire.Tools)
	}

	if len(bytes.TrimSpace(wire.ToolChoice)) > 0 && !bytes.Equal(bytes.TrimSpace(wire.ToolChoice), []byte("null")) {
		tc, err := decodeToolChoice(wire.ToolChoice)
		if err != nil {
			return nil, err
		}
		req.ToolChoice = tc
	}

	return req, nil
}

func decodeRequestInput(rawInput json.RawMessage, req *ir.Request) error {
	trimmed := bytes.TrimSpace(rawInput)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}

	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return fmt.Errorf("openairesponses: decode string input: %w", err)
		}
		req.Messages = append(req.Messages, ir.Message{
			Role: ir.RoleUser,
			Parts: []ir.ContentPart{
				{Type: ir.PartText, Text: text},
			},
		})
		return nil
	}

	if trimmed[0] != '[' {
		return fmt.Errorf("openairesponses: input must be a string or array")
	}

	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return fmt.Errorf("openairesponses: decode input array: %w", err)
	}

	for idx, rawItem := range items {
		itemTrimmed := bytes.TrimSpace(rawItem)
		if len(itemTrimmed) == 0 || bytes.Equal(itemTrimmed, []byte("null")) {
			continue
		}
		var item responsesInputItemWire
		if err := json.Unmarshal(itemTrimmed, &item); err != nil {
			return fmt.Errorf("openairesponses: decode input[%d]: %w", idx, err)
		}

		itemType := item.Type
		if itemType == "" && item.Role != "" {
			itemType = "message"
		}

		switch itemType {
		case "message":
			role := mapWireRoleToIR(item.Role)
			parts, err := decodeMessageContent(item.Content)
			if err != nil {
				return fmt.Errorf("openairesponses: decode input[%d].content: %w", idx, err)
			}
			if role == ir.RoleAssistant {
				appendAssistantParts(req, parts...)
			} else {
				req.Messages = append(req.Messages, ir.Message{
					Role:  role,
					Parts: parts,
				})
			}

		case "function_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			appendAssistantParts(req, ir.ContentPart{
				Type: ir.PartToolCall,
				ToolCall: &ir.ToolCall{
					ID:        callID,
					ItemID:    item.ID,
					Name:      item.Name,
					Arguments: item.Arguments,
					Kind:      "function",
				},
			})

		case "custom_tool_call":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			args := item.Input
			if args == "" {
				args = item.Arguments
			}
			appendAssistantParts(req, ir.ContentPart{
				Type: ir.PartToolCall,
				ToolCall: &ir.ToolCall{
					ID:        callID,
					ItemID:    item.ID,
					Name:      item.Name,
					Arguments: args,
					Kind:      "custom",
				},
			})

		case "function_call_output", "custom_tool_call_output":
			callID := item.CallID
			if callID == "" {
				callID = item.ID
			}
			tr, err := decodeToolOutput(callID, item.Output)
			if err != nil {
				return fmt.Errorf("openairesponses: decode input[%d].output: %w", idx, err)
			}
			part := ir.ContentPart{
				Type:       ir.PartToolResult,
				ToolResult: tr,
			}
			if itemType == "custom_tool_call_output" {
				part.ProviderMetadata = map[string]any{"item_type": "custom_tool_call_output"}
			}
			req.Messages = append(req.Messages, ir.Message{
				Role:  ir.RoleTool,
				Parts: []ir.ContentPart{part},
			})

		case "reasoning":
			reasoning := decodeReasoningItem(item.ID, item.Summary, item.Content, item.EncryptedContent)
			appendAssistantParts(req, ir.ContentPart{
				Type:      ir.PartReasoning,
				Reasoning: reasoning,
			})

		default:
			req.PassthroughItems = append(req.PassthroughItems, ir.PassthroughItem{
				OriginFormat: ir.FormatOpenAIResponses,
				Position:     idx,
				Kind:         itemType,
				Payload:      cloneRaw(itemTrimmed),
			})
		}
	}

	return nil
}

func appendAssistantParts(req *ir.Request, parts ...ir.ContentPart) {
	if len(parts) == 0 {
		return
	}
	if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == ir.RoleAssistant {
		req.Messages[n-1].Parts = append(req.Messages[n-1].Parts, parts...)
		return
	}
	req.Messages = append(req.Messages, ir.Message{
		Role:  ir.RoleAssistant,
		Parts: append([]ir.ContentPart(nil), parts...),
	})
}

func mapWireRoleToIR(role string) ir.Role {
	switch strings.ToLower(role) {
	case "system":
		return ir.RoleSystem
	case "developer":
		return ir.RoleDeveloper
	case "assistant":
		return ir.RoleAssistant
	case "tool":
		return ir.RoleTool
	default:
		return ir.RoleUser
	}
}

func decodeMessageContent(raw json.RawMessage) ([]ir.ContentPart, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, err
		}
		return []ir.ContentPart{{Type: ir.PartText, Text: text}}, nil
	}
	if trimmed[0] != '[' {
		return nil, fmt.Errorf("unexpected content JSON: %s", string(trimmed))
	}

	var wireParts []responsesContentPartWire
	if err := json.Unmarshal(trimmed, &wireParts); err != nil {
		return nil, err
	}

	parts := make([]ir.ContentPart, 0, len(wireParts))
	for _, wp := range wireParts {
		parts = append(parts, decodeContentPart(wp)...)
	}
	return parts, nil
}

func decodeContentPart(wp responsesContentPartWire) []ir.ContentPart {
	switch wp.Type {
	case "input_text", "output_text", "text", "":
		out := []ir.ContentPart{{
			Type: ir.PartText,
			Text: wp.Text,
		}}
		for _, ann := range wp.Annotations {
			if cit := decodeAnnotation(ann); cit != nil {
				out = append(out, ir.ContentPart{
					Type:     ir.PartCitation,
					Citation: cit,
				})
			}
		}
		return out

	case "refusal":
		refusal := wp.Refusal
		if refusal == "" {
			refusal = wp.Text
		}
		return []ir.ContentPart{{
			Type:    ir.PartRefusal,
			Refusal: refusal,
		}}

	case "input_image", "image_url":
		img := decodeInputImage(wp.ImageURL, wp.Detail)
		if img == nil {
			return nil
		}
		return []ir.ContentPart{{
			Type:  ir.PartImage,
			Image: img,
		}}

	case "input_file":
		f := &ir.FileSource{
			FileID:   wp.FileID,
			Filename: wp.Filename,
			URL:      wp.FileURL,
		}
		if wp.FileData != "" {
			if mediaType, data, ok := ir.ParseDataURL(wp.FileData); ok {
				f.MediaType = mediaType
				f.Data = data
			} else {
				f.Data = wp.FileData
			}
		}
		return []ir.ContentPart{{
			Type: ir.PartFile,
			File: f,
		}}

	case "input_audio":
		if wp.InputAudio == nil {
			return nil
		}
		return []ir.ContentPart{{
			Type: ir.PartAudio,
			Audio: &ir.AudioSource{
				Data:   wp.InputAudio.Data,
				Format: wp.InputAudio.Format,
			},
		}}

	default:
		if wp.Text != "" {
			return []ir.ContentPart{{Type: ir.PartText, Text: wp.Text}}
		}
		return nil
	}
}

func decodeInputImage(rawURL json.RawMessage, topDetail string) *ir.ImageSource {
	trimmed := bytes.TrimSpace(rawURL)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var urlStr string
	detail := topDetail
	if trimmed[0] == '"' {
		_ = json.Unmarshal(trimmed, &urlStr)
	} else if trimmed[0] == '{' {
		var obj struct {
			URL    string `json:"url"`
			Detail string `json:"detail"`
		}
		if err := json.Unmarshal(trimmed, &obj); err == nil {
			urlStr = obj.URL
			if detail == "" {
				detail = obj.Detail
			}
		}
	}
	if urlStr == "" {
		return nil
	}
	if mediaType, data, ok := ir.ParseDataURL(urlStr); ok {
		return &ir.ImageSource{
			Data:      data,
			MediaType: mediaType,
			Detail:    detail,
		}
	}
	return &ir.ImageSource{
		URL:    urlStr,
		Detail: detail,
	}
}

func decodeAnnotation(ann responsesAnnotationWire) *ir.Citation {
	if ann.URL == "" && ann.Title == "" && ann.CitedText == "" && ann.Text == "" {
		return nil
	}
	cited := ann.CitedText
	if cited == "" {
		cited = ann.Text
	}
	return &ir.Citation{
		URL:        ann.URL,
		Title:      ann.Title,
		CitedText:  cited,
		StartIndex: ann.StartIndex,
		EndIndex:   ann.EndIndex,
	}
}

func decodeToolOutput(callID string, rawOutput json.RawMessage) (*ir.ToolResult, error) {
	tr := &ir.ToolResult{ToolCallID: callID}
	trimmed := bytes.TrimSpace(rawOutput)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return tr, nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return nil, err
		}
		tr.Content = s
		return tr, nil
	}
	if trimmed[0] == '[' {
		var wireParts []responsesContentPartWire
		if err := json.Unmarshal(trimmed, &wireParts); err != nil {
			return nil, err
		}
		parts := make([]ir.ContentPart, 0, len(wireParts))
		for _, wp := range wireParts {
			parts = append(parts, decodeContentPart(wp)...)
		}
		tr.Parts = parts
		tr.Content = ir.JoinTextParts(parts, "\n")
		return tr, nil
	}
	// Fallback: preserve structured JSON values as canonical JSON string.
	tr.Content = string(trimmed)
	return tr, nil
}

func decodeReasoningItem(itemID string, summary []responsesReasoningPart, rawContent json.RawMessage, encrypted string) *ir.Reasoning {
	summaryText := joinReasoningParts(summary)

	var contentParts []responsesReasoningPart
	trimmed := bytes.TrimSpace(rawContent)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		_ = json.Unmarshal(trimmed, &contentParts)
	}
	contentText := joinReasoningParts(contentParts)

	text := contentText
	if text == "" {
		text = summaryText
	}
	return &ir.Reasoning{
		Text:         text,
		Summary:      summaryText,
		RedactedData: encrypted,
		OriginFormat: ir.FormatOpenAIResponses,
		ItemID:       itemID,
	}
}

func joinReasoningParts(parts []responsesReasoningPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

func decodeRequestTools(rawTools []json.RawMessage) []ir.ToolDefinition {
	out := make([]ir.ToolDefinition, 0, len(rawTools))
	for _, rawTool := range rawTools {
		trimmed := bytes.TrimSpace(rawTool)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		var wire responsesToolWire
		if err := json.Unmarshal(trimmed, &wire); err != nil {
			continue
		}
		switch wire.Type {
		case "function":
			name := wire.Name
			desc := wire.Description
			params := wire.Parameters
			strict := wire.Strict
			if name == "" && wire.Function != nil {
				name = wire.Function.Name
				if desc == "" {
					desc = wire.Function.Description
				}
				if len(params) == 0 {
					params = wire.Function.Parameters
				}
				if strict == nil {
					strict = wire.Function.Strict
				}
			}
			out = append(out, ir.ToolDefinition{
				Type:         "function",
				Name:         name,
				Description:  desc,
				Parameters:   cloneRaw(params),
				Strict:       strict,
				OriginFormat: ir.FormatOpenAIResponses,
			})
		case "custom":
			out = append(out, ir.ToolDefinition{
				Type:         "custom",
				Name:         wire.Name,
				Description:  wire.Description,
				NativeRaw:    cloneRaw(trimmed),
				OriginFormat: ir.FormatOpenAIResponses,
			})
		default:
			out = append(out, ir.ToolDefinition{
				Type:         wire.Type,
				Name:         wire.Name,
				NativeRaw:    cloneRaw(trimmed),
				OriginFormat: ir.FormatOpenAIResponses,
			})
		}
	}
	return out
}

func decodeToolChoice(raw json.RawMessage) (*ir.ToolChoice, error) {
	trimmed := bytes.TrimSpace(raw)
	if trimmed[0] == '"' {
		var mode string
		if err := json.Unmarshal(trimmed, &mode); err != nil {
			return nil, fmt.Errorf("openairesponses: decode tool_choice string: %w", err)
		}
		return &ir.ToolChoice{Mode: mode}, nil
	}
	var obj responsesToolChoiceObj
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("openairesponses: decode tool_choice object: %w", err)
	}
	switch obj.Type {
	case "function", "custom":
		return &ir.ToolChoice{
			Mode:     "tool",
			ToolName: obj.Name,
		}, nil
	case "allowed_tools":
		names := make([]string, 0, len(obj.Tools))
		for _, t := range obj.Tools {
			if t.Name != "" {
				names = append(names, t.Name)
			}
		}
		mode := obj.Mode
		if mode == "" {
			mode = "auto"
		}
		return &ir.ToolChoice{
			Mode:         mode,
			AllowedTools: names,
		}, nil
	default:
		if obj.Name != "" {
			return &ir.ToolChoice{Mode: "tool", ToolName: obj.Name}, nil
		}
		if obj.Type != "" {
			return &ir.ToolChoice{Mode: "tool", ToolName: obj.Type}, nil
		}
		return &ir.ToolChoice{Mode: "auto"}, nil
	}
}

// EncodeRequest serializes a provider-neutral IR Request into an OpenAI Responses API JSON payload.
func (c *Codec) EncodeRequest(req *ir.Request, ctx *ir.ConversionContext) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("openairesponses: nil IR request")
	}

	out := map[string]any{
		"model": req.Model,
	}

	if instructions := ir.JoinTextParts(req.System, "\n\n"); instructions != "" {
		out["instructions"] = instructions
	}

	inputItems := encodeRequestInput(req, ctx)
	out["input"] = inputItems

	customToolNames := make(map[string]struct{})
	if len(req.Tools) > 0 {
		tools := make([]any, 0, len(req.Tools))
		for _, td := range req.Tools {
			switch {
			case td.Type == "function" || td.Type == "":
				fn := map[string]any{
					"type": "function",
					"name": td.Name,
				}
				if td.Description != "" {
					fn["description"] = td.Description
				}
				if len(bytes.TrimSpace(td.Parameters)) > 0 {
					fn["parameters"] = td.Parameters
				}
				if td.Strict != nil {
					fn["strict"] = *td.Strict
				}
				tools = append(tools, fn)

			case td.Type == "custom":
				customToolNames[td.Name] = struct{}{}
				if len(td.NativeRaw) > 0 && shouldPreserveResponsesNative(td.OriginFormat, ctx) {
					tools = append(tools, td.NativeRaw)
				} else {
					ct := map[string]any{
						"type": "custom",
						"name": td.Name,
					}
					if td.Description != "" {
						ct["description"] = td.Description
					}
					tools = append(tools, ct)
				}

			case len(td.NativeRaw) > 0 && shouldPreserveResponsesNative(td.OriginFormat, ctx):
				tools = append(tools, td.NativeRaw)

			default:
				ctx.AddWarning("dropped_foreign_native_tool", "tools", fmt.Sprintf("dropped non-Responses native tool %q (origin=%s)", td.Type, td.OriginFormat))
			}
		}
		if len(tools) > 0 {
			out["tools"] = tools
		}
	}

	if req.ToolChoice != nil {
		if tc := encodeToolChoice(req.ToolChoice, customToolNames); tc != nil {
			out["tool_choice"] = tc
		}
	}

	if req.ParallelToolCalls != nil {
		out["parallel_tool_calls"] = *req.ParallelToolCalls
	} else if req.ToolChoice != nil && req.ToolChoice.DisableParallel != nil {
		out["parallel_tool_calls"] = !*req.ToolChoice.DisableParallel
	}

	g := req.Generation
	if g.MaxTokens != nil {
		out["max_output_tokens"] = *g.MaxTokens
	}
	if g.Temperature != nil {
		out["temperature"] = *g.Temperature
	}
	if g.TopP != nil {
		out["top_p"] = *g.TopP
	}
	if g.User != "" {
		out["user"] = g.User
	}
	if g.Store != nil {
		out["store"] = *g.Store
	}
	if g.Truncation != "" {
		out["truncation"] = g.Truncation
	}
	if g.PreviousResponseID != "" {
		out["previous_response_id"] = g.PreviousResponseID
	}
	if g.ServiceTier != "" {
		out["service_tier"] = g.ServiceTier
	}
	if g.PromptCacheKey != "" {
		out["prompt_cache_key"] = g.PromptCacheKey
	}
	if g.PromptCacheRetention != "" {
		out["prompt_cache_retention"] = g.PromptCacheRetention
	}

	if req.Reasoning != nil {
		if rCfg := encodeReasoningConfig(req.Reasoning); rCfg != nil {
			out["reasoning"] = rCfg
		}
	}

	if req.ResponseFormat != nil || g.Verbosity != "" {
		textObj := map[string]any{}
		if g.Verbosity != "" {
			textObj["verbosity"] = g.Verbosity
		}
		if req.ResponseFormat != nil && req.ResponseFormat.Type != "" {
			fmtObj := map[string]any{
				"type": req.ResponseFormat.Type,
			}
			if req.ResponseFormat.Name != "" {
				fmtObj["name"] = req.ResponseFormat.Name
			}
			if req.ResponseFormat.Description != "" {
				fmtObj["description"] = req.ResponseFormat.Description
			}
			if len(bytes.TrimSpace(req.ResponseFormat.Schema)) > 0 {
				fmtObj["schema"] = req.ResponseFormat.Schema
			}
			if req.ResponseFormat.Strict != nil {
				fmtObj["strict"] = *req.ResponseFormat.Strict
			}
			textObj["format"] = fmtObj
		}
		if len(textObj) > 0 {
			out["text"] = textObj
		}
	}

	if req.Stream {
		out["stream"] = true
	}
	if len(req.Include) > 0 {
		out["include"] = req.Include
	}
	if len(req.Metadata) > 0 {
		out["metadata"] = req.Metadata
	}

	return json.Marshal(out)
}

func encodeRequestInput(req *ir.Request, ctx *ir.ConversionContext) []any {
	items := make([]any, 0, len(req.Messages)+len(req.PassthroughItems))
	toolCounter := 0

	for _, msg := range req.Messages {
		switch msg.Role {
		case ir.RoleSystem, ir.RoleDeveloper:
			content := encodeUserLikeContent(msg.Parts)
			if len(content) > 0 {
				items = append(items, map[string]any{
					"type":    "message",
					"role":    "developer",
					"content": content,
				})
			}

		case ir.RoleUser:
			var pending []any
			flushUser := func() {
				if len(pending) == 0 {
					return
				}
				items = append(items, map[string]any{
					"type":    "message",
					"role":    "user",
					"content": pending,
				})
				pending = nil
			}
			for _, part := range msg.Parts {
				if part.Type == ir.PartToolResult && part.ToolResult != nil {
					flushUser()
					items = append(items, encodeToolResultItem(part))
					continue
				}
				if encoded := encodeUserContentPart(part); encoded != nil {
					pending = append(pending, encoded)
				}
			}
			flushUser()

		case ir.RoleAssistant:
			var pendingAssistant []any
			flushAssistant := func() {
				if len(pendingAssistant) == 0 {
					return
				}
				items = append(items, map[string]any{
					"type":    "message",
					"role":    "assistant",
					"status":  "completed",
					"content": pendingAssistant,
				})
				pendingAssistant = nil
			}

			for _, part := range msg.Parts {
				switch part.Type {
				case ir.PartText:
					pendingAssistant = append(pendingAssistant, map[string]any{
						"type": "output_text",
						"text": part.Text,
					})

				case ir.PartRefusal:
					pendingAssistant = append(pendingAssistant, map[string]any{
						"type":    "refusal",
						"refusal": part.Refusal,
					})

				case ir.PartReasoning:
					if part.Reasoning == nil {
						continue
					}
					flushAssistant()
					if item := encodeRequestReasoningItem(part.Reasoning, ctx); item != nil {
						items = append(items, item)
					}

				case ir.PartToolCall:
					if part.ToolCall == nil {
						continue
					}
					flushAssistant()
					items = append(items, encodeRequestToolCallItem(part.ToolCall, toolCounter))
					toolCounter++
				}
			}
			flushAssistant()

		case ir.RoleTool:
			for _, part := range msg.Parts {
				if part.Type == ir.PartToolResult && part.ToolResult != nil {
					items = append(items, encodeToolResultItem(part))
				}
			}
		}
	}

	// Re-insert same-format passthrough items in positional order; warn & drop foreign ones.
	var nativePassthrough []ir.PassthroughItem
	for _, pt := range req.PassthroughItems {
		if len(pt.Payload) == 0 {
			continue
		}
		if shouldPreserveResponsesNative(pt.OriginFormat, ctx) {
			nativePassthrough = append(nativePassthrough, pt)
		} else {
			ctx.AddWarning("dropped_foreign_passthrough_item", "input", fmt.Sprintf("dropped non-Responses passthrough item %q (origin=%s)", pt.Kind, pt.OriginFormat))
		}
	}
	if len(nativePassthrough) > 0 {
		sort.SliceStable(nativePassthrough, func(i, j int) bool {
			return nativePassthrough[i].Position < nativePassthrough[j].Position
		})
		for _, pt := range nativePassthrough {
			pos := pt.Position
			if pos < 0 || pos >= len(items) {
				items = append(items, pt.Payload)
				continue
			}
			items = append(items, nil)
			copy(items[pos+1:], items[pos:])
			items[pos] = pt.Payload
		}
	}

	return items
}

func encodeUserLikeContent(parts []ir.ContentPart) []any {
	content := make([]any, 0, len(parts))
	for _, part := range parts {
		if encoded := encodeUserContentPart(part); encoded != nil {
			content = append(content, encoded)
		}
	}
	return content
}

func encodeUserContentPart(part ir.ContentPart) any {
	switch part.Type {
	case ir.PartText:
		return map[string]any{
			"type": "input_text",
			"text": part.Text,
		}
	case ir.PartImage:
		if part.Image == nil {
			return nil
		}
		url := part.Image.URL
		if url == "" && part.Image.Data != "" {
			url = buildDataURL(part.Image.MediaType, part.Image.Data)
		}
		if url == "" {
			return nil
		}
		img := map[string]any{
			"type":      "input_image",
			"image_url": url,
		}
		if part.Image.Detail != "" {
			img["detail"] = part.Image.Detail
		}
		return img
	case ir.PartFile:
		if part.File == nil {
			return nil
		}
		f := map[string]any{
			"type": "input_file",
		}
		if part.File.FileID != "" {
			f["file_id"] = part.File.FileID
		}
		if part.File.Filename != "" {
			f["filename"] = part.File.Filename
		}
		if part.File.URL != "" {
			f["file_url"] = part.File.URL
		}
		if part.File.Data != "" {
			if part.File.MediaType != "" && !strings.HasPrefix(part.File.Data, "data:") {
				f["file_data"] = buildDataURL(part.File.MediaType, part.File.Data)
			} else {
				f["file_data"] = part.File.Data
			}
		}
		return f
	case ir.PartAudio:
		if part.Audio == nil || part.Audio.Data == "" {
			return nil
		}
		return map[string]any{
			"type": "input_audio",
			"input_audio": map[string]any{
				"data":   part.Audio.Data,
				"format": part.Audio.Format,
			},
		}
	default:
		return nil
	}
}

func encodeRequestReasoningItem(r *ir.Reasoning, ctx *ir.ConversionContext) map[string]any {
	isNativeOrigin := r.OriginFormat == ir.FormatOpenAIResponses
	if !isNativeOrigin && (r.Signature != "" || r.RedactedData != "") {
		ctx.AddWarning(
			"dropped_foreign_reasoning_opaque",
			"input.reasoning",
			fmt.Sprintf("dropped foreign reasoning signature/redacted_data from origin %q", r.OriginFormat),
		)
	}

	var encrypted string
	if isNativeOrigin && r.RedactedData != "" && shouldPreserveResponsesNative(r.OriginFormat, ctx) {
		encrypted = r.RedactedData
	}

	// Never inject synthetic unsigned reasoning items from other providers, as OpenAI Responses
	// rejects unsigned reasoning items with HTTP 400.
	if !isNativeOrigin && encrypted == "" && r.ItemID == "" {
		ctx.AddWarning(
			"skipped_synthetic_reasoning_item",
			"input.reasoning",
			"skipped cross-provider reasoning item without OpenAI Responses item_id or encrypted_content",
		)
		return nil
	}

	summaryText := r.Summary
	if summaryText == "" {
		summaryText = r.Text
	}
	summary := make([]map[string]any, 0, 1)
	if summaryText != "" {
		summary = append(summary, map[string]any{
			"type": "summary_text",
			"text": summaryText,
		})
	}

	item := map[string]any{
		"type":    "reasoning",
		"summary": summary,
	}
	if r.ItemID != "" {
		item["id"] = r.ItemID
	}
	if encrypted != "" {
		item["encrypted_content"] = encrypted
	}
	return item
}

func encodeRequestToolCallItem(tc *ir.ToolCall, idx int) map[string]any {
	callID := tc.ID
	if callID == "" {
		callID = fmt.Sprintf("call_%d", idx)
	}
	fcID := tc.ItemID
	if fcID == "" {
		fcID = synthesizeFunctionCallItemID(callID, idx)
	}

	if tc.Kind == "custom" {
		return map[string]any{
			"type":    "custom_tool_call",
			"id":      fcID,
			"call_id": callID,
			"name":    tc.Name,
			"input":   tc.Arguments,
		}
	}

	args := tc.Arguments
	if args == "" {
		args = "{}"
	}
	return map[string]any{
		"type":      "function_call",
		"id":        fcID,
		"call_id":   callID,
		"name":      tc.Name,
		"arguments": args,
	}
}

func encodeToolResultItem(part ir.ContentPart) map[string]any {
	tr := part.ToolResult
	itemType := "function_call_output"
	if part.ProviderMetadata != nil {
		if t, ok := part.ProviderMetadata["item_type"].(string); ok && t == "custom_tool_call_output" {
			itemType = "custom_tool_call_output"
		}
	}

	var output any
	if hasMultimodalToolParts(tr.Parts) {
		arr := make([]any, 0, len(tr.Parts))
		for _, p := range tr.Parts {
			if encoded := encodeUserContentPart(p); encoded != nil {
				arr = append(arr, encoded)
			}
		}
		output = arr
	} else if tr.Content != "" {
		output = tr.Content
	} else if len(tr.Parts) > 0 {
		output = ir.JoinTextParts(tr.Parts, "\n")
	} else {
		output = ""
	}

	return map[string]any{
		"type":    itemType,
		"call_id": tr.ToolCallID,
		"output":  output,
	}
}

func hasMultimodalToolParts(parts []ir.ContentPart) bool {
	for _, p := range parts {
		if p.Type == ir.PartImage || p.Type == ir.PartFile || p.Type == ir.PartAudio {
			return true
		}
	}
	return false
}

func encodeToolChoice(tc *ir.ToolChoice, customTools map[string]struct{}) any {
	if tc == nil {
		return nil
	}
	if len(tc.AllowedTools) > 0 {
		mode := tc.Mode
		if mode == "" || mode == "any" {
			mode = "required"
		}
		tools := make([]map[string]any, 0, len(tc.AllowedTools))
		for _, name := range tc.AllowedTools {
			t := "function"
			if _, ok := customTools[name]; ok {
				t = "custom"
			}
			tools = append(tools, map[string]any{
				"type": t,
				"name": name,
			})
		}
		return map[string]any{
			"type":  "allowed_tools",
			"mode":  mode,
			"tools": tools,
		}
	}

	switch tc.Mode {
	case "auto", "none":
		return tc.Mode
	case "required", "any":
		return "required"
	case "tool":
		if tc.ToolName == "" {
			return "required"
		}
		t := "function"
		if _, ok := customTools[tc.ToolName]; ok {
			t = "custom"
		}
		return map[string]any{
			"type": t,
			"name": tc.ToolName,
		}
	default:
		return nil
	}
}

func encodeReasoningConfig(rc *ir.ReasoningConfig) map[string]any {
	effort := rc.Effort
	if effort == "" {
		if (rc.Enabled != nil && !*rc.Enabled) || rc.Mode == "disabled" {
			effort = "none"
		} else if rc.BudgetTokens != nil && *rc.BudgetTokens > 0 {
			switch {
			case *rc.BudgetTokens <= 2048:
				effort = "low"
			case *rc.BudgetTokens <= 8192:
				effort = "medium"
			default:
				effort = "high"
			}
		} else if (rc.Enabled != nil && *rc.Enabled) || rc.Mode == "enabled" || rc.Mode == "adaptive" {
			effort = "medium"
		}
	}

	out := map[string]any{}
	if effort != "" {
		out["effort"] = effort
	}
	if rc.Summary != "" {
		out["summary"] = rc.Summary
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
