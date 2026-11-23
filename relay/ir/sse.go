package ir

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
)

const (
	defaultSSEInitBuf = 64 * 1024
	defaultSSEMaxBuf  = 64 * 1024 * 1024
)

// FormatSSEFrame encodes an SSEFrame into standard "\n\n"-delimited SSE wire bytes.
func FormatSSEFrame(frame SSEFrame) []byte {
	var buf bytes.Buffer
	if frame.Event != "" {
		buf.WriteString("event: ")
		buf.WriteString(frame.Event)
		buf.WriteByte('\n')
	}
	if len(frame.Data) > 0 {
		// Handle multi-line data safely per SSE specification.
		lines := bytes.Split(frame.Data, []byte("\n"))
		for _, line := range lines {
			buf.WriteString("data: ")
			buf.Write(line)
			buf.WriteByte('\n')
		}
	}
	buf.WriteByte('\n')
	return buf.Bytes()
}

// SSEReader reads complete SSE frames while preserving both event names and multi-line data payloads.
type SSEReader struct {
	reader *bufio.Reader
	maxBuf int
}

// NewSSEReader wraps an io.Reader with a streaming SSE frame parser.
func NewSSEReader(r io.Reader) *SSEReader {
	return &SSEReader{
		reader: bufio.NewReaderSize(r, defaultSSEInitBuf),
		maxBuf: defaultSSEMaxBuf,
	}
}

// Next returns the next SSE frame (event name, data bytes).
// Returns io.EOF when the underlying stream ends cleanly.
func (s *SSEReader) Next() (eventName string, data []byte, err error) {
	var dataBuf bytes.Buffer
	hasData := false

	for {
		line, readErr := s.readLine()
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", nil, readErr
		}

		// Strip optional CR before LF.
		line = strings.TrimSuffix(line, "\r")

		if line == "" {
			if hasData {
				return eventName, bytes.Clone(dataBuf.Bytes()), nil
			}
			eventName = ""
			if errors.Is(readErr, io.EOF) {
				return "", nil, io.EOF
			}
			continue
		}

		// Ignore SSE comment lines (e.g., ": PING" or OpenRouter keep-alives).
		if strings.HasPrefix(line, ":") {
			if errors.Is(readErr, io.EOF) {
				if hasData {
					return eventName, bytes.Clone(dataBuf.Bytes()), nil
				}
				return "", nil, io.EOF
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			payload := strings.TrimPrefix(line, "data:")
			if strings.HasPrefix(payload, " ") {
				payload = payload[1:]
			}
			if hasData {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(payload)
			hasData = true
		}

		if errors.Is(readErr, io.EOF) {
			if hasData {
				return eventName, bytes.Clone(dataBuf.Bytes()), nil
			}
			return "", nil, io.EOF
		}
	}
}

func (s *SSEReader) readLine() (string, error) {
	var full bytes.Buffer
	for {
		fragment, isPrefix, err := s.reader.ReadLine()
		if len(fragment) > 0 {
			if full.Len()+len(fragment) > s.maxBuf {
				return "", errors.New("ir: SSE line exceeds maximum buffer size")
			}
			full.Write(fragment)
		}
		if err != nil {
			if errors.Is(err, io.EOF) && full.Len() > 0 {
				return full.String(), io.EOF
			}
			return "", err
		}
		if !isPrefix {
			return full.String(), nil
		}
	}
}
