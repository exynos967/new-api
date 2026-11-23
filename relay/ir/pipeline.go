package ir

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

// PipelineConfig configures a Pipeline translation execution.
type PipelineConfig struct {
	Registry     *Registry
	Bridge       *TurnBridge
	BridgeScope  string
	SourceFormat Format
	TargetFormat Format
	SourceShim   *Shim
	TargetShim   *Shim
	MetadataMode MetadataMode
	Options      map[string]any
}

// Pipeline orchestrates bidirectional Source <-> IR <-> Target conversions,
// declarative shims, and cross-turn metadata bridging.
type Pipeline struct {
	registry    *Registry
	bridge      *TurnBridge
	bridgeScope string
	source      Codec
	target      Codec
	sourceShim  *Shim
	targetShim  *Shim
	mode        MetadataMode
	options     map[string]any
}

// NewPipeline validates formats and builds a reusable Pipeline.
func NewPipeline(cfg PipelineConfig) (*Pipeline, error) {
	reg := cfg.Registry
	if reg == nil {
		reg = DefaultRegistry()
	}
	srcFmt := cfg.SourceFormat
	if srcFmt == FormatUnknown && cfg.SourceShim != nil {
		srcFmt = cfg.SourceShim.BaseFormat
	}
	dstFmt := cfg.TargetFormat
	if dstFmt == FormatUnknown && cfg.TargetShim != nil {
		dstFmt = cfg.TargetShim.BaseFormat
	}
	srcCodec, err := reg.Get(srcFmt)
	if err != nil {
		return nil, fmt.Errorf("ir: source codec: %w", err)
	}
	dstCodec, err := reg.Get(dstFmt)
	if err != nil {
		return nil, fmt.Errorf("ir: target codec: %w", err)
	}
	mode := cfg.MetadataMode
	if mode == "" {
		if srcFmt == dstFmt {
			mode = MetadataPreserve
		} else {
			mode = MetadataStrip
		}
	}
	opts := make(map[string]any, len(cfg.Options))
	for k, v := range cfg.Options {
		opts[k] = v
	}
	return &Pipeline{
		registry:    reg,
		bridge:      cfg.Bridge,
		bridgeScope: cfg.BridgeScope,
		source:      srcCodec,
		target:      dstCodec,
		sourceShim:  cfg.SourceShim,
		targetShim:  cfg.TargetShim,
		mode:        mode,
		options:     opts,
	}, nil
}

// ConvertRequest translates a raw request body from SourceFormat to TargetFormat.
// It returns the target JSON bytes, the intermediate Request, and the ConversionContext (including warnings).
func (p *Pipeline) ConvertRequest(raw []byte) ([]byte, *Request, *ConversionContext, error) {
	ctx := NewConversionContext(p.source.Format(), p.target.Format())
	ctx.MetadataMode = p.mode
	ctx.BridgeScope = p.bridgeScope
	for k, v := range p.options {
		ctx.Options[k] = v
	}

	in := raw
	if p.sourceShim != nil && len(p.sourceShim.PreIRRequestTransforms) > 0 {
		var err error
		in, err = ApplyJSONTransforms(in, p.sourceShim.PreIRRequestTransforms, ctx)
		if err != nil {
			return nil, nil, ctx, fmt.Errorf("ir: source pre-IR request shim: %w", err)
		}
	}

	irReq, err := p.source.DecodeRequest(in, ctx)
	if err != nil {
		return nil, nil, ctx, fmt.Errorf("ir: decode %s request: %w", p.source.Format(), err)
	}

	if p.bridge != nil {
		loaded := p.bridge.LoadForRequest(p.bridgeScope, irReq)
		ctx.BridgeState = loaded
		loaded.InjectIntoRequest(irReq)
	}

	out, err := p.target.EncodeRequest(irReq, ctx)
	if err != nil {
		return nil, irReq, ctx, fmt.Errorf("ir: encode %s request: %w", p.target.Format(), err)
	}

	if p.targetShim != nil && len(p.targetShim.PostIRRequestTransforms) > 0 {
		out, err = ApplyJSONTransforms(out, p.targetShim.PostIRRequestTransforms, ctx)
		if err != nil {
			return nil, irReq, ctx, fmt.Errorf("ir: target post-IR request shim: %w", err)
		}
	}
	return out, irReq, ctx, nil
}

// ConvertResponse translates a non-streaming response body from TargetFormat back to SourceFormat.
func (p *Pipeline) ConvertResponse(raw []byte, fallbackModel string) ([]byte, *Response, *ConversionContext, error) {
	ctx := NewConversionContext(p.target.Format(), p.source.Format())
	ctx.MetadataMode = p.mode
	ctx.BridgeScope = p.bridgeScope
	for k, v := range p.options {
		ctx.Options[k] = v
	}

	in := raw
	if p.targetShim != nil && len(p.targetShim.PreIRResponseTransforms) > 0 {
		var err error
		in, err = ApplyJSONTransforms(in, p.targetShim.PreIRResponseTransforms, ctx)
		if err != nil {
			return nil, nil, ctx, fmt.Errorf("ir: target pre-IR response shim: %w", err)
		}
	}

	irResp, err := p.target.DecodeResponse(in, ctx)
	if err != nil {
		return nil, nil, ctx, fmt.Errorf("ir: decode %s response: %w", p.target.Format(), err)
	}
	if irResp.Model == "" {
		irResp.Model = fallbackModel
	}

	if p.bridge != nil {
		ctx.BridgeState.ExtractResponseBridge(irResp, p.target.Format())
		p.bridge.Save(p.bridgeScope, ctx.BridgeState)
	}

	out, err := p.source.EncodeResponse(irResp, ctx)
	if err != nil {
		return nil, irResp, ctx, fmt.Errorf("ir: encode %s response: %w", p.source.Format(), err)
	}

	if p.sourceShim != nil && len(p.sourceShim.PostIRResponseTransforms) > 0 {
		out, err = ApplyJSONTransforms(out, p.sourceShim.PostIRResponseTransforms, ctx)
		if err != nil {
			return nil, irResp, ctx, fmt.Errorf("ir: source post-IR response shim: %w", err)
		}
	}
	return out, irResp, ctx, nil
}

// StreamTranslator translates a single upstream SSE stream (in TargetFormat)
// into downstream SSE frames (in SourceFormat) with full stateful context tracking.
type StreamTranslator struct {
	pipeline  *Pipeline
	decodeCtx *StreamContext
	encodeCtx *StreamContext
}

// NewStreamTranslator initializes a stateful translator for one streaming response.
func (p *Pipeline) NewStreamTranslator(fallbackModel string) *StreamTranslator {
	decodeCtx := NewStreamContext(p.target.Format(), p.source.Format())
	decodeCtx.MetadataMode = p.mode
	decodeCtx.BridgeScope = p.bridgeScope
	decodeCtx.Model = fallbackModel
	for k, v := range p.options {
		decodeCtx.Options[k] = v
	}

	encodeCtx := NewStreamContext(p.target.Format(), p.source.Format())
	encodeCtx.MetadataMode = p.mode
	encodeCtx.BridgeScope = p.bridgeScope
	encodeCtx.Model = fallbackModel
	for k, v := range p.options {
		encodeCtx.Options[k] = v
	}

	return &StreamTranslator{
		pipeline:  p,
		decodeCtx: decodeCtx,
		encodeCtx: encodeCtx,
	}
}

// ProcessChunk ingests one upstream SSE event (or Bedrock JSON chunk) and returns zero or more downstream SSEFrames.
func (st *StreamTranslator) ProcessChunk(sseEvent string, rawData []byte) ([]SSEFrame, error) {
	trimmed := bytes.TrimSpace(rawData)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if bytes.Equal(trimmed, []byte("[DONE]")) {
		return st.Flush()
	}

	st.decodeCtx.ReceivedChunks++
	events, err := st.pipeline.target.DecodeStreamEvent(sseEvent, trimmed, st.decodeCtx)
	if err != nil {
		return nil, err
	}

	var frames []SSEFrame
	for _, ev := range events {
		st.recordStreamAccounting(ev)
		out, err := st.pipeline.source.EncodeStreamEvent(ev, st.encodeCtx)
		if err != nil {
			return frames, err
		}
		frames = append(frames, out...)
	}
	st.encodeCtx.EmittedFrames += len(frames)
	return frames, nil
}

// Flush closes open blocks, emits terminal events, and persists captured cross-turn bridge metadata.
func (st *StreamTranslator) Flush() ([]SSEFrame, error) {
	if st.encodeCtx.Ended {
		return nil, nil
	}
	frames, err := st.pipeline.source.FlushStream(st.encodeCtx)
	st.encodeCtx.Ended = true
	st.encodeCtx.EmittedFrames += len(frames)
	st.persistBridge()
	return frames, err
}

// Usage returns the accumulated Usage observed across the stream (nil if upstream reported none).
func (st *StreamTranslator) Usage() *Usage {
	if st.encodeCtx.LastUsage != nil {
		return st.encodeCtx.LastUsage
	}
	return st.decodeCtx.LastUsage
}

// EstimatedOutputText returns all streamed text, reasoning, and tool argument fragments concatenated
// for fallback token estimation when an upstream omits usage metrics.
func (st *StreamTranslator) EstimatedOutputText() string {
	return st.decodeCtx.OutputTextEstimate.String()
}

// ReceivedChunks returns the count of non-empty upstream chunks processed.
func (st *StreamTranslator) ReceivedChunks() int {
	return st.decodeCtx.ReceivedChunks
}

// Warnings returns all conversion warnings accumulated by decode and encode contexts.
func (st *StreamTranslator) Warnings() []Warning {
	out := make([]Warning, 0, len(st.decodeCtx.Warnings)+len(st.encodeCtx.Warnings))
	out = append(out, st.decodeCtx.Warnings...)
	out = append(out, st.encodeCtx.Warnings...)
	return out
}

// TranslateReader streams an entire upstream SSE io.Reader, invoking emit for each downstream SSEFrame.
func (st *StreamTranslator) TranslateReader(r io.Reader, emit func(frame SSEFrame) error) error {
	reader := NewSSEReader(r)
	for {
		evName, data, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		frames, procErr := st.ProcessChunk(evName, data)
		if procErr != nil {
			return procErr
		}
		for _, f := range frames {
			if emitErr := emit(f); emitErr != nil {
				return emitErr
			}
		}
	}
	tail, flushErr := st.Flush()
	if flushErr != nil {
		return flushErr
	}
	for _, f := range tail {
		if emitErr := emit(f); emitErr != nil {
			return emitErr
		}
	}
	return nil
}

func (st *StreamTranslator) recordStreamAccounting(ev StreamEvent) {
	if ev.ResponseID != "" && st.encodeCtx.ResponseID == "" {
		st.encodeCtx.ResponseID = ev.ResponseID
	}
	if ev.Model != "" && st.encodeCtx.Model == "" {
		st.encodeCtx.Model = ev.Model
	}
	if ev.Created > 0 && st.encodeCtx.Created == 0 {
		st.encodeCtx.Created = ev.Created
	}
	if ev.Text != "" {
		st.decodeCtx.OutputTextEstimate.WriteString(ev.Text)
	}
	if ev.Reasoning != "" {
		st.decodeCtx.OutputTextEstimate.WriteString(ev.Reasoning)
	}
	if ev.ToolArguments != "" {
		st.decodeCtx.OutputTextEstimate.WriteString(ev.ToolArguments)
	}
	if ev.Usage != nil {
		st.decodeCtx.MergeUsage(ev.Usage)
		st.encodeCtx.MergeUsage(ev.Usage)
	}
	if ev.Type == EventToolCallStart && ev.ToolCallID != "" {
		st.decodeCtx.BridgeState.RecordToolCall(ToolCallBridgeEntry{
			ToolCallID:       ev.ToolCallID,
			ResponsesItemID:  ev.ToolCallItemID,
			ToolName:         ev.ToolName,
			ThoughtSignature: ev.ThoughtSignature,
			OriginFormat:     st.pipeline.target.Format(),
		})
	}
}

func (st *StreamTranslator) persistBridge() {
	if st.pipeline.bridge == nil {
		return
	}
	st.pipeline.bridge.Save(st.pipeline.bridgeScope, st.decodeCtx.BridgeState)
}
