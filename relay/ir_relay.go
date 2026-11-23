package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/openaimodel"
	"github.com/QuantumNous/new-api/relay/channel"
	awsChannel "github.com/QuantumNous/new-api/relay/channel/aws"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/ir"
	_ "github.com/QuantumNous/new-api/relay/ir/codec/all"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type irTargetSpec struct {
	Format             ir.Format
	Shim               *ir.Shim
	RelayMode          int
	URLPath            string
	RelayFormat        types.RelayFormat
	ModelRewrite       string
	ClearStreamOptions bool
	IsBedrockClaude    bool
}

func isIRSupportedRelayMode(sourceFormat ir.Format, mode int) bool {
	switch sourceFormat {
	case ir.FormatOpenAIChat:
		return mode == relayconstant.RelayModeChatCompletions
	case ir.FormatOpenAIResponses:
		return mode == relayconstant.RelayModeResponses
	case ir.FormatAnthropic:
		return mode == relayconstant.RelayModeUnknown || mode == relayconstant.RelayModeChatCompletions
	case ir.FormatGoogleGenAI:
		return mode == relayconstant.RelayModeGemini || mode == relayconstant.RelayModeUnknown
	default:
		return false
	}
}

// resolveTargetIRSpec determines the upstream canonical wire protocol and shim for adaptor.
// Returns ok=false when the channel/model should stay on its native or specialized legacy path.
func resolveTargetIRSpec(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, sourceFormat ir.Format) (irTargetSpec, bool) {
	if info == nil || adaptor == nil || sourceFormat == ir.FormatUnknown {
		return irTargetSpec{}, false
	}

	// Channels with bespoke transport / wrapping stay on their dedicated paths.
	switch info.ChannelType {
	case constant.ChannelTypeCline,
		constant.ChannelTypeKilo,
		constant.ChannelTypeGMICloud,
		constant.ChannelTypeXunfeiMaas,
		constant.ChannelTypeBilibiliWeb:
		return irTargetSpec{}, false
	case constant.ChannelTypeOpenCode, constant.ChannelTypeOpenCodeGo:
		// OpenCode free/chat/claude paths have dedicated stream guards and tool handling.
		// Only bridge Gemini -> OpenCode Responses where opencode/adaptor.go otherwise returns 400.
		if sourceFormat == ir.FormatGoogleGenAI {
			ep := constant.GetOpenCodeEndpoint(info.UpstreamModelName)
			if info.ChannelType == constant.ChannelTypeOpenCodeGo {
				ep = constant.GetOpenCodeGoEndpoint(info.UpstreamModelName)
			}
			if ep == constant.OpenCodeEndpointResponses {
				return irTargetSpec{
					Format:      ir.FormatOpenAIResponses,
					RelayMode:   relayconstant.RelayModeResponses,
					URLPath:     "/v1/responses",
					RelayFormat: types.RelayFormatOpenAIResponses,
				}, true
			}
		}
		return irTargetSpec{}, false
	}

	switch info.ApiType {
	case constant.APITypeCodex:
		return irTargetSpec{
			Format:      ir.FormatOpenAIResponses,
			Shim:        ir.CodexResponsesShim,
			RelayMode:   relayconstant.RelayModeResponses,
			URLPath:     "/v1/responses",
			RelayFormat: types.RelayFormatOpenAIResponses,
		}, true

	case constant.APITypeOpenRouter:
		if !info.ChannelOtherSettings.IsOpenRouterEnterprise() {
			// OpenRouter native natively supports Chat, Responses, and Claude Messages.
			if sourceFormat == ir.FormatOpenAIChat || sourceFormat == ir.FormatOpenAIResponses || sourceFormat == ir.FormatAnthropic {
				return irTargetSpec{}, false
			}
			if shouldChatCompletionsUseResponses(info) {
				return irTargetSpec{
					Format:      ir.FormatOpenAIResponses,
					RelayMode:   relayconstant.RelayModeResponses,
					URLPath:     "/v1/responses",
					RelayFormat: types.RelayFormatOpenAIResponses,
				}, true
			}
			return irTargetSpec{
				Format:      ir.FormatOpenAIChat,
				RelayMode:   relayconstant.RelayModeChatCompletions,
				URLPath:     "/v1/chat/completions",
				RelayFormat: types.RelayFormatOpenAI,
			}, true
		}
		if sourceFormat == ir.FormatOpenAIResponses {
			return irTargetSpec{}, false
		}
		if shouldChatCompletionsUseResponses(info) {
			return irTargetSpec{
				Format:      ir.FormatOpenAIResponses,
				RelayMode:   relayconstant.RelayModeResponses,
				URLPath:     "/v1/responses",
				RelayFormat: types.RelayFormatOpenAIResponses,
			}, true
		}
		return irTargetSpec{
			Format:      ir.FormatOpenAIChat,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/chat/completions",
			RelayFormat: types.RelayFormatOpenAI,
		}, true

	case constant.APITypeAnthropic:
		return irTargetSpec{
			Format:      ir.FormatAnthropic,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/messages",
			RelayFormat: types.RelayFormatClaude,
		}, true

	case constant.APITypeGemini:
		model := info.UpstreamModelName
		if strings.HasPrefix(model, "imagen") ||
			strings.HasPrefix(model, "text-embedding") ||
			strings.HasPrefix(model, "embedding") ||
			strings.HasPrefix(model, "gemini-embedding") {
			return irTargetSpec{}, false
		}
		return irTargetSpec{
			Format:      ir.FormatGoogleGenAI,
			RelayMode:   relayconstant.RelayModeGemini,
			RelayFormat: types.RelayFormatGemini,
		}, true

	case constant.APITypeVertexAi:
		va, ok := adaptor.(*vertex.Adaptor)
		if !ok {
			return irTargetSpec{}, false
		}
		target, ok := va.ResolveIRTarget(info)
		if !ok {
			return irTargetSpec{}, false
		}
		spec := irTargetSpec{
			Format:             target.Format,
			Shim:               target.Shim,
			RelayMode:          target.RelayMode,
			ModelRewrite:       target.ModelRewrite,
			ClearStreamOptions: target.ClearStream,
		}
		switch target.Format {
		case ir.FormatAnthropic:
			spec.RelayFormat = types.RelayFormatClaude
			spec.URLPath = "/v1/messages"
		case ir.FormatGoogleGenAI:
			spec.RelayFormat = types.RelayFormatGemini
		case ir.FormatOpenAIResponses:
			spec.RelayFormat = types.RelayFormatOpenAIResponses
			spec.URLPath = "/v1/responses"
		default:
			spec.RelayFormat = types.RelayFormatOpenAI
			spec.URLPath = "/v1/chat/completions"
		}
		return spec, true

	case constant.APITypeAws:
		aa, ok := adaptor.(*awsChannel.Adaptor)
		if !ok || !aa.IsBedrockClaudeModel(info) {
			return irTargetSpec{}, false
		}
		return irTargetSpec{
			Format:          ir.FormatAnthropic,
			Shim:            ir.BedrockClaudeShim,
			RelayMode:       relayconstant.RelayModeChatCompletions,
			URLPath:         "/v1/messages",
			RelayFormat:     types.RelayFormatClaude,
			IsBedrockClaude: true,
		}, true

	case constant.APITypeOpenAI:
		// Official OpenAI Chat (including Chat->Responses) stays on its official compatibility path
		// to preserve prompt_cache_breakpoint & audio validation contracts.
		if info.ChannelType == constant.ChannelTypeOpenAI && sourceFormat == ir.FormatOpenAIChat {
			return irTargetSpec{}, false
		}
		if sourceFormat == ir.FormatOpenAIResponses {
			// Native OpenAI / Azure / OpenAI-compatible Responses endpoint: same protocol.
			return irTargetSpec{}, false
		}
		endpoint := constant.EndpointType(common.GetContextKeyString(c, constant.ContextKeyPlaygroundEndpoint))
		if endpoint != constant.EndpointTypeOpenAI && endpoint != constant.EndpointTypeCohereChat {
			useResponses := shouldChatCompletionsUseResponses(info)
			if !useResponses && info.ChannelType == constant.ChannelTypeOpenAI {
				if _, _, caps, known := openaimodel.Resolve(info.UpstreamModelName); known && (caps.ResponsesOnly || caps.Tools == openaimodel.ResponsesTools || caps.Tools == openaimodel.ResponsesReasoningTools) {
					useResponses = true
				}
			}
			if useResponses {
				return irTargetSpec{
					Format:      ir.FormatOpenAIResponses,
					RelayMode:   relayconstant.RelayModeResponses,
					URLPath:     "/v1/responses",
					RelayFormat: types.RelayFormatOpenAIResponses,
				}, true
			}
		}
		return irTargetSpec{
			Format:      ir.FormatOpenAIChat,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/chat/completions",
			RelayFormat: types.RelayFormatOpenAI,
		}, true

	case constant.APITypeDeepSeek:
		// DeepSeek natively supports Chat, Claude Messages, and Responses.
		if sourceFormat != ir.FormatGoogleGenAI {
			return irTargetSpec{}, false
		}
		return irTargetSpec{
			Format:      ir.FormatOpenAIChat,
			Shim:        ir.DeepSeekChatShim,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/chat/completions",
			RelayFormat: types.RelayFormatOpenAI,
		}, true

	case constant.APITypeVLLM,
		constant.APITypeLiteLLM:
		// Self-hosted vLLM/LiteLLM natively support both OpenAI Chat and Responses endpoints.
		if sourceFormat == ir.FormatOpenAIChat || sourceFormat == ir.FormatOpenAIResponses {
			return irTargetSpec{}, false
		}
		return irTargetSpec{
			Format:      ir.FormatOpenAIChat,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/chat/completions",
			RelayFormat: types.RelayFormatOpenAI,
		}, true

	case constant.APITypeOllama,
		constant.APITypeCerebras,
		constant.APITypeSiliconFlow,
		constant.APITypeMoonshot,
		constant.APITypeMiniMax,
		constant.APITypeZhipu,
		constant.APITypeZhipuV4,
		constant.APITypeBaiduV2,
		constant.APITypeTencent,
		constant.APITypeSubmodel:
		return irTargetSpec{
			Format:      ir.FormatOpenAIChat,
			RelayMode:   relayconstant.RelayModeChatCompletions,
			URLPath:     "/v1/chat/completions",
			RelayFormat: types.RelayFormatOpenAI,
		}, true

	case constant.APITypeAli,
		constant.APITypeVolcEngine,
		constant.APITypeXai,
		constant.APITypePerplexity,
		constant.APITypeCloudflare,
		constant.APITypeQianwenAI,
		constant.APITypeMiMo,
		constant.APITypeAgnesAI,
		constant.APITypePoe,
		constant.APITypeXinference:
		// When admin explicitly enables ChatCompletionsToResponsesPolicy for this channel/model,
		// translate any non-Responses source format directly into OpenAI Responses via IR.
		if sourceFormat != ir.FormatOpenAIResponses && shouldChatCompletionsUseResponses(info) {
			return irTargetSpec{
				Format:      ir.FormatOpenAIResponses,
				RelayMode:   relayconstant.RelayModeResponses,
				URLPath:     "/v1/responses",
				RelayFormat: types.RelayFormatOpenAIResponses,
			}, true
		}
		// These channels natively support OpenAI Chat and (mostly) Responses/Claude;
		// bridge unsupported cross-protocol entries (e.g., Gemini -> Chat) via IR.
		if sourceFormat == ir.FormatGoogleGenAI {
			return irTargetSpec{
				Format:      ir.FormatOpenAIChat,
				RelayMode:   relayconstant.RelayModeChatCompletions,
				URLPath:     "/v1/chat/completions",
				RelayFormat: types.RelayFormatOpenAI,
			}, true
		}
		return irTargetSpec{}, false
	}

	return irTargetSpec{}, false
}

// tryIRCrossProtocolRelay intercepts cross-protocol dialogue requests when global.ir_conversion_enabled
// is enabled and translates both request and response/SSE stream through the neutral IR engine.
// Returns handled=false when the request is same-protocol, IR is disabled, or the channel uses a bespoke path.
func tryIRCrossProtocolRelay(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	adaptor channel.Adaptor,
	sourceFormat ir.Format,
	sourceReq any,
) (handled bool, usage *dto.Usage, apiErr *types.NewAPIError) {
	if !model_setting.IsIRConversionEnabled() {
		return false, nil, nil
	}
	if info == nil || adaptor == nil || sourceReq == nil || !isIRSupportedRelayMode(sourceFormat, info.RelayMode) {
		return false, nil, nil
	}

	spec, ok := resolveTargetIRSpec(c, info, adaptor, sourceFormat)
	if !ok || spec.Format == sourceFormat {
		return false, nil, nil
	}

	srcBytes, err := common.Marshal(sourceReq)
	if err != nil {
		return true, nil, types.NewError(fmt.Errorf("ir: marshal source request failed: %w", err), types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	// Ensure Gemini source JSON has model/stream populated from RelayInfo before IR decoding.
	if sourceFormat == ir.FormatGoogleGenAI {
		srcBytes, err = ir.ApplyJSONTransforms(srcBytes, []ir.JSONTransform{
			ir.SetDefaultField("model", info.UpstreamModelName),
		}, nil)
		if err != nil {
			return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
	}

	scope := ""
	if info.ChannelId > 0 {
		scope = fmt.Sprintf("channel:%d", info.ChannelId)
	}

	opts := map[string]any{
		"attach_thought_signature": model_setting.GetGeminiSettings().FunctionCallThoughtSignatureEnabled,
		"include_usage":            info.ShouldIncludeUsage,
		"disable_stream_options":   !info.SupportStreamOptions || spec.ClearStreamOptions,
	}
	if dm := model_setting.GetClaudeSettings().GetDefaultMaxTokens(info.UpstreamModelName); dm > 0 {
		opts["default_max_tokens"] = dm
	}

	pipeline, err := ir.NewPipeline(ir.PipelineConfig{
		Bridge:       ir.DefaultTurnBridge(),
		BridgeScope:  scope,
		SourceFormat: sourceFormat,
		TargetFormat: spec.Format,
		TargetShim:   spec.Shim,
		Options:      opts,
	})
	if err != nil {
		return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	targetJSON, _, convCtx, err := pipeline.ConvertRequest(srcBytes)
	if err != nil {
		return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if convCtx != nil && len(convCtx.Warnings) > 0 && common.DebugEnabled {
		logger.LogDebug(c, fmt.Sprintf("ir request warnings (%s->%s): %+v", sourceFormat, spec.Format, convCtx.Warnings))
	}

	// Align model and stream fields when translating from URL-driven dialects (e.g. Google GenAI)
	// or applying Vertex OpenSource model rewrites.
	targetJSON, err = finalizeIRTargetRequestJSON(targetJSON, info, spec)
	if err != nil {
		return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	info.AppendRequestConversion(spec.RelayFormat)
	info.FinalRequestRelayFormat = spec.RelayFormat

	if spec.Format == ir.FormatOpenAIResponses {
		var respReq dto.OpenAIResponsesRequest
		if uErr := common.Unmarshal(targetJSON, &respReq); uErr == nil {
			info.InitResponsesUsageInfo(&respReq)
		}
	}

	if spec.Format != ir.FormatGoogleGenAI {
		targetJSON, err = relaycommon.RemoveDisabledFields(targetJSON, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
	}

	if len(info.ParamOverride) > 0 {
		targetJSON, err = relaycommon.ApplyParamOverrideWithRelayInfo(targetJSON, info)
		if err != nil {
			return true, nil, newAPIErrorFromParamOverride(err)
		}
	}

	if info.ChannelType == constant.ChannelTypeOpenAI {
		if spec.Format == ir.FormatOpenAIResponses {
			targetJSON, err = normalizeOfficialResponsesRequest(info, targetJSON)
			if err != nil {
				return true, nil, invalidOpenAIModelRequest(err)
			}
		} else if spec.Format == ir.FormatOpenAIChat {
			var requiresResponses bool
			targetJSON, requiresResponses, err = normalizeOfficialChatRequest(info, targetJSON)
			if err != nil {
				return true, nil, invalidOpenAIModelRequest(err)
			}
			if requiresResponses {
				spec.Format = ir.FormatOpenAIResponses
				spec.RelayMode = relayconstant.RelayModeResponses
				spec.URLPath = "/v1/responses"
				spec.RelayFormat = types.RelayFormatOpenAIResponses
				info.AppendRequestConversion(spec.RelayFormat)
				info.FinalRequestRelayFormat = spec.RelayFormat
				upgradePipe, pErr := ir.NewPipeline(ir.PipelineConfig{
					Bridge:       ir.DefaultTurnBridge(),
					BridgeScope:  scope,
					SourceFormat: ir.FormatOpenAIChat,
					TargetFormat: ir.FormatOpenAIResponses,
					Options:      opts,
				})
				if pErr != nil {
					return true, nil, types.NewError(pErr, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
				}
				targetJSON, _, _, err = upgradePipe.ConvertRequest(targetJSON)
				if err != nil {
					return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
				}
				targetJSON, err = normalizeOfficialResponsesRequest(info, targetJSON)
				if err != nil {
					return true, nil, invalidOpenAIModelRequest(err)
				}
				pipeline, err = ir.NewPipeline(ir.PipelineConfig{
					Bridge:       ir.DefaultTurnBridge(),
					BridgeScope:  scope,
					SourceFormat: sourceFormat,
					TargetFormat: ir.FormatOpenAIResponses,
					Options:      opts,
				})
				if err != nil {
					return true, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
				}
			}
		}
	}

	relaycommon.SetReasoningEffortFromRequest(info, targetJSON)
	relaycommon.SetConversationUpstreamRequest(info, targetJSON)

	clientModel := info.ClientModelName
	if clientModel == "" {
		clientModel = info.OriginModelName
	}

	if spec.IsBedrockClaude {
		u, bErr := executeIRBedrockClaude(c, info, adaptor.(*awsChannel.Adaptor), pipeline, targetJSON, clientModel)
		return true, u, bErr
	}

	u, hErr := executeIRHTTPRelay(c, info, adaptor, pipeline, spec, targetJSON, clientModel)
	return true, u, hErr
}

func finalizeIRTargetRequestJSON(raw []byte, info *relaycommon.RelayInfo, spec irTargetSpec) ([]byte, error) {
	if spec.Format == ir.FormatGoogleGenAI {
		return raw, nil
	}
	var transforms []ir.JSONTransform
	if spec.ModelRewrite != "" {
		transforms = append(transforms, ir.SetField("model", spec.ModelRewrite))
	} else if spec.Shim != ir.VertexClaudeShim && spec.Shim != ir.BedrockClaudeShim {
		transforms = append(transforms, ir.SetDefaultField("model", info.UpstreamModelName))
	}
	if spec.Shim != ir.BedrockClaudeShim {
		if info.IsStream {
			transforms = append(transforms, ir.SetField("stream", true))
		} else {
			transforms = append(transforms, ir.SetDefaultField("stream", false))
		}
	}
	if len(transforms) == 0 {
		return raw, nil
	}
	return ir.ApplyJSONTransforms(raw, transforms, nil)
}

func executeIRBedrockClaude(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	awsAdaptor *awsChannel.Adaptor,
	pipeline *ir.Pipeline,
	targetJSON []byte,
	clientModel string,
) (*dto.Usage, *types.NewAPIError) {
	preparedBody, err := awsAdaptor.PrepareRawClaudePayload(c, info, targetJSON)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	relaycommon.SetConversationUpstreamRequest(info, preparedBody)

	if rpmRes := service.TryAcquireChannelRPM(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection); !rpmRes.Allowed {
		return nil, service.NewChannelRPMLimitError("")
	}

	if info.IsStream {
		helper.SetEventStreamHeaders(c)
		st := pipeline.NewStreamTranslator(clientModel)
		emitErr := awsAdaptor.InvokeRawClaudeStream(c, info, preparedBody, func(chunk []byte) *types.NewAPIError {
			info.ReceivedResponseCount++
			frames, procErr := st.ProcessChunk("", chunk)
			if procErr != nil {
				return types.NewError(procErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
			}
			for _, f := range frames {
				if wErr := writeIRSSEFrame(c, info, f); wErr != nil {
					return types.NewError(wErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
				}
			}
			return nil
		})
		if emitErr != nil {
			if emitErr.StatusCode == http.StatusTooManyRequests {
				service.RecordChannelRPM429(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection)
			}
			return nil, emitErr
		}
		tail, flushErr := st.Flush()
		if flushErr != nil {
			return nil, types.NewError(flushErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
		}
		for _, f := range tail {
			if wErr := writeIRSSEFrame(c, info, f); wErr != nil {
				return nil, types.NewError(wErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
			}
		}
		return finalizeIRUsage(c, info, st.Usage(), st.EstimatedOutputText(), st.ReceivedChunks(), ir.FormatAnthropic), nil
	}

	respBody, apiErr := awsAdaptor.InvokeRawClaudeNonStream(c, info, preparedBody)
	if apiErr != nil {
		if apiErr.StatusCode == http.StatusTooManyRequests {
			service.RecordChannelRPM429(c.Request.Context(), info.ChannelId, info.ChannelSetting.RPMProtection)
		}
		return nil, apiErr
	}
	return handleIRNonStreamResponse(c, info, pipeline, respBody, clientModel, ir.FormatAnthropic)
}

func executeIRHTTPRelay(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	adaptor channel.Adaptor,
	pipeline *ir.Pipeline,
	spec irTargetSpec,
	targetJSON []byte,
	clientModel string,
) (*dto.Usage, *types.NewAPIError) {
	origRelayMode := info.RelayMode
	origURLPath := info.RequestURLPath
	info.RelayMode = spec.RelayMode
	if spec.URLPath != "" {
		info.RequestURLPath = spec.URLPath
	}
	defer func() {
		info.RelayMode = origRelayMode
		info.RequestURLPath = origURLPath
	}()

	resp, err := doChannelRPMGuardedRequest(c, info, func() (any, error) {
		return adaptor.DoRequest(c, info, bytes.NewReader(targetJSON))
	})
	if err != nil {
		if apiErr, ok := err.(*types.NewAPIError); ok {
			return nil, apiErr
		}
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	httpResp, ok := resp.(*http.Response)
	if !ok || httpResp == nil {
		return nil, types.NewError(fmt.Errorf("ir: unexpected response type %T", resp), types.ErrorCodeDoRequestFailed)
	}

	info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")

	if httpResp.StatusCode != http.StatusOK {
		newApiErr := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		service.ResetStatusCode(newApiErr, c.GetString("status_code_mapping"))
		return nil, newApiErr
	}

	relaycommon.WrapConversationUpstreamResponse(info, httpResp)
	defer httpResp.Body.Close()

	if info.IsStream {
		helper.SetEventStreamHeaders(c)
		st := pipeline.NewStreamTranslator(clientModel)
		streamErr := st.TranslateReader(httpResp.Body, func(frame ir.SSEFrame) error {
			info.SetFirstResponseTime()
			return writeIRSSEFrame(c, info, frame)
		})
		info.ReceivedResponseCount += st.ReceivedChunks()
		if streamErr != nil {
			return nil, types.NewError(streamErr, types.ErrorCodeBadResponseBody, types.ErrOptionWithSkipRetry())
		}
		return finalizeIRUsage(c, info, st.Usage(), st.EstimatedOutputText(), st.ReceivedChunks(), spec.Format), nil
	}

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadResponseBodyFailed)
	}
	return handleIRNonStreamResponse(c, info, pipeline, respBody, clientModel, spec.Format)
}

func handleIRNonStreamResponse(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	pipeline *ir.Pipeline,
	respBody []byte,
	clientModel string,
	targetFormat ir.Format,
) (*dto.Usage, *types.NewAPIError) {
	outBytes, irResp, _, err := pipeline.ConvertResponse(respBody, clientModel)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}

	c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.Writer.WriteHeader(http.StatusOK)
	service.IOCopyBytesGracefully(c, nil, outBytes)
	info.SendResponseCount++

	estimatedText := ""
	if irResp != nil {
		var sb strings.Builder
		for _, ch := range irResp.Choices {
			for _, p := range ch.Message.Parts {
				if p.Type == ir.PartText && p.Text != "" {
					sb.WriteString(p.Text)
				} else if p.Type == ir.PartReasoning && p.Reasoning != nil {
					sb.WriteString(p.Reasoning.Text)
				} else if p.Type == ir.PartToolCall && p.ToolCall != nil {
					sb.WriteString(p.ToolCall.Arguments)
				}
			}
		}
		estimatedText = sb.String()
	}

	var irUsage *ir.Usage
	if irResp != nil {
		irUsage = irResp.Usage
	}
	return finalizeIRUsage(c, info, irUsage, estimatedText, 1, targetFormat), nil
}

func writeIRSSEFrame(c *gin.Context, info *relaycommon.RelayInfo, frame ir.SSEFrame) error {
	raw := ir.FormatSSEFrame(frame)
	relaycommon.AppendConversationClientResponse(c, raw)
	if _, err := c.Writer.Write(raw); err != nil {
		return err
	}
	c.Writer.Flush()
	info.SendResponseCount++
	return nil
}

func finalizeIRUsage(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	irUsage *ir.Usage,
	estimatedText string,
	receivedChunks int,
	targetFormat ir.Format,
) *dto.Usage {
	var dtoUsage *dto.Usage
	if irUsage != nil {
		dtoUsage = irUsage.ToDTOUsage()
		if targetFormat == ir.FormatAnthropic && irUsage.WebSearchRequests > 0 {
			c.Set("claude_web_search_requests", irUsage.WebSearchRequests)
		}
		if targetFormat == ir.FormatOpenAIResponses && info.ResponsesUsageInfo != nil {
			if irUsage.WebSearchRequests > 0 {
				if info.ResponsesUsageInfo.BuiltInTools == nil {
					info.ResponsesUsageInfo.BuiltInTools = make(map[string]*relaycommon.BuildInToolInfo)
				}
				info.ResponsesUsageInfo.BuiltInTools["web_search_preview"] = &relaycommon.BuildInToolInfo{
					ToolName:  "web_search_preview",
					CallCount: irUsage.WebSearchRequests,
				}
			}
			if irUsage.FileSearchCalls > 0 {
				if info.ResponsesUsageInfo.BuiltInTools == nil {
					info.ResponsesUsageInfo.BuiltInTools = make(map[string]*relaycommon.BuildInToolInfo)
				}
				info.ResponsesUsageInfo.BuiltInTools["file_search"] = &relaycommon.BuildInToolInfo{
					ToolName:  "file_search",
					CallCount: irUsage.FileSearchCalls,
				}
			}
		}
		if irUsage.HasImageGenCall {
			c.Set("image_generation_call", true)
		}
	} else {
		dtoUsage = &dto.Usage{}
		if targetFormat == ir.FormatAnthropic {
			dtoUsage.UsageSemantic = "anthropic"
		} else {
			dtoUsage.UsageSemantic = "openai"
		}
	}

	// Preserve existing cache token fields while backfilling missing prompt/completion estimates.
	if receivedChunks > 0 && (dtoUsage.CompletionTokens == 0 || (dtoUsage.PromptTokens == 0 && dtoUsage.PromptTokensDetails.CachedTokens == 0 && dtoUsage.PromptTokensDetails.GetCacheCreationTokens() == 0)) {
		est := service.ResponseText2Usage(c, estimatedText, info.UpstreamModelName, info.GetEstimatePromptTokens())
		if dtoUsage.PromptTokens == 0 && dtoUsage.PromptTokensDetails.CachedTokens == 0 && dtoUsage.PromptTokensDetails.GetCacheCreationTokens() == 0 {
			dtoUsage.PromptTokens = est.PromptTokens
		}
		if dtoUsage.CompletionTokens == 0 {
			dtoUsage.CompletionTokens = est.CompletionTokens
		}
		if dtoUsage.TotalTokens == 0 {
			dtoUsage.TotalTokens = dtoUsage.PromptTokens + dtoUsage.CompletionTokens
		}
	}
	return dtoUsage
}

func billIRUsage(c *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) {
	if usage == nil {
		usage = &dto.Usage{}
	}
	containAudioTokens := usage.CompletionTokenDetails.AudioTokens > 0 || usage.PromptTokensDetails.AudioTokens > 0
	containsAudioRatios := ratio_setting.ContainsAudioRatio(info.OriginModelName) || ratio_setting.ContainsAudioCompletionRatio(info.OriginModelName)
	if containAudioTokens && containsAudioRatios {
		service.PostAudioConsumeQuota(c, info, usage, "")
		return
	}
	service.PostTextConsumeQuota(c, info, usage, nil)
}
