package vertex

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// Google Vertex AI 共享服务（Shared Requests）调度的请求头及其取值。
// 参考 ST-Vertex-PayGo：PayGo / Flex / Priority 均通过请求头进行调度声明。
const (
	headerVertexRequestType       = "X-Vertex-AI-LLM-Request-Type"
	headerVertexSharedRequestType = "X-Vertex-AI-LLM-Shared-Request-Type"
	headerVertexServerTimeout     = "X-Server-Timeout"
	vertexRequestTypeShared       = "shared"
	// Flex 排队时间较长，放宽上游服务端超时，避免排队期间被提前断开
	vertexFlexServerTimeoutSeconds = "1800"
)

// useSharedScheduling 当前请求是否需要进入 Vertex 共享按量调度（PayGo / Flex / Priority）。
// 仅对 gemini-* 官方原生模型生效，其余发布方（Claude/Llama 等）官方不支持层级切换，直接走原通道。
func (a *Adaptor) useSharedScheduling(info *relaycommon.RelayInfo) bool {
	if a.RequestMode != RequestModeGemini || !strings.HasPrefix(info.UpstreamModelName, "gemini") {
		return false
	}
	return info.ChannelOtherSettings.VertexPayGoOnly || a.useNonStandardTier(info)
}

// useNonStandardTier 当前请求是否启用了非标准层级（Flex / Priority）。
func (a *Adaptor) useNonStandardTier(info *relaycommon.RelayInfo) bool {
	if a.RequestMode != RequestModeGemini {
		return false
	}
	if !strings.HasPrefix(info.UpstreamModelName, "gemini") {
		return false
	}
	tier := info.ChannelOtherSettings.VertexServiceTier
	return tier == dto.VertexServiceTierFlex || tier == dto.VertexServiceTierPriority
}

// applyServiceTierHeaders 按渠道配置注入 Vertex 服务层级与 PayGo 调度请求头。
func (a *Adaptor) applyServiceTierHeaders(req *http.Header, info *relaycommon.RelayInfo) {
	if !a.useSharedScheduling(info) {
		return
	}
	// PayGo 与 Flex / Priority 均需显式声明走共享按量通道（绕过预置吞吐量）
	req.Set(headerVertexRequestType, vertexRequestTypeShared)
	if !a.useNonStandardTier(info) {
		return
	}
	tier := info.ChannelOtherSettings.VertexServiceTier
	req.Set(headerVertexSharedRequestType, string(tier))
	if tier == dto.VertexServiceTierFlex {
		req.Set(headerVertexServerTimeout, vertexFlexServerTimeoutSeconds)
	}
}

// serviceTierRequiresGlobal 非标准层级由 Google 在全球池调度，官方要求区域固定为 global。
func (a *Adaptor) serviceTierRequiresGlobal(info *relaycommon.RelayInfo) bool {
	return a.useNonStandardTier(info)
}
