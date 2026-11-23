package openairesponses

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/relay/ir"
)

// Codec implements bidirectional translation between OpenAI Responses API (/v1/responses)
// wire payloads/SSE streams and the provider-neutral IR.
type Codec struct{}

var _ ir.Codec = (*Codec)(nil)

// New creates a new OpenAI Responses API IR codec.
func New() *Codec {
	return &Codec{}
}

// Format returns the wire format handled by this codec.
func (c *Codec) Format() ir.Format {
	return ir.FormatOpenAIResponses
}

// cloneRaw returns a safe independent copy of a json.RawMessage.
func cloneRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cp := make(json.RawMessage, len(raw))
	copy(cp, raw)
	return cp
}

// sanitizeID normalizes an arbitrary identifier to alphanumeric, underscore, and hyphen characters.
func sanitizeID(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			b.WriteByte(ch)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// synthesizeFunctionCallItemID ensures an OpenAI Responses function_call item id begins with "fc_".
func synthesizeFunctionCallItemID(callID string, fallbackIdx int) string {
	clean := sanitizeID(callID)
	if clean == "" {
		return fmt.Sprintf("fc_%d", fallbackIdx)
	}
	if strings.HasPrefix(clean, "fc_") {
		return clean
	}
	return "fc_" + clean
}

// ensureResponseID ensures an OpenAI Responses envelope id begins with "resp_".
func ensureResponseID(id string) string {
	if id == "" {
		return fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	if strings.HasPrefix(id, "resp_") {
		return id
	}
	clean := sanitizeID(id)
	if clean == "" {
		return fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	return "resp_" + clean
}

// shortIDSuffix returns a compact suffix derived from an envelope ID for item IDs (msg_*, rs_*).
func shortIDSuffix(respID string) string {
	trimmed := strings.TrimPrefix(respID, "resp_")
	trimmed = sanitizeID(trimmed)
	if len(trimmed) > 24 {
		trimmed = trimmed[:24]
	}
	if trimmed == "" {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return trimmed
}

// shouldPreserveResponsesNative reports whether OpenAI Responses native opaque fields should be emitted.
func shouldPreserveResponsesNative(origin ir.Format, ctx *ir.ConversionContext) bool {
	if origin != ir.FormatOpenAIResponses {
		return false
	}
	if ctx == nil || ctx.TargetFormat == ir.FormatUnknown {
		return true
	}
	return ctx.ShouldPreserveMetadata(origin)
}

// buildDataURL formats raw base64 data into a data:<media>;base64,<payload> URI.
func buildDataURL(mediaType, data string) string {
	if strings.HasPrefix(data, "data:") {
		return data
	}
	if mediaType == "" {
		mediaType = "image/png"
	}
	return "data:" + mediaType + ";base64," + data
}
