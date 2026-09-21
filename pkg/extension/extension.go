// Package extension defines ProxySieve's versioned external-process protocol.
package extension

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	APIVersion    = 1
	MaxFrameBytes = 1 << 20
)

var (
	ErrFrameTooLarge = errors.New("extension frame is too large")
	ErrInvalidFrame  = errors.New("invalid extension frame")
)

type Capability string

const (
	ProxyProvider Capability = "proxy-provider"
	Selector      Capability = "selector"
	AlertSink     Capability = "alert-sink"
)

func (c Capability) Valid() bool {
	return c == ProxyProvider || c == Selector || c == AlertSink
}

type Descriptor struct {
	Name         string       `json:"name,omitempty"`
	Version      string       `json:"version,omitempty"`
	APIVersion   int          `json:"api_version,omitempty"`
	Capabilities []Capability `json:"capabilities,omitempty"`
}

func (d Descriptor) Validate() error {
	if !validToken(d.Name, 128) || !validToken(d.Version, 64) || d.APIVersion != APIVersion || len(d.Capabilities) == 0 || len(d.Capabilities) > 16 {
		return ErrInvalidFrame
	}
	seen := make(map[Capability]bool, len(d.Capabilities))
	for _, capability := range d.Capabilities {
		if !capability.Valid() || seen[capability] {
			return ErrInvalidFrame
		}
		seen[capability] = true
	}
	return nil
}

func (d Descriptor) Has(capability Capability) bool {
	for _, current := range d.Capabilities {
		if current == capability {
			return true
		}
	}
	return false
}

type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (e *ProtocolError) Valid() bool {
	return e != nil && validToken(e.Code, 64) && len(e.Message) <= 512
}

type Message struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Descriptor
	Capability Capability      `json:"capability,omitempty"`
	Method     string          `json:"method,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      *ProtocolError  `json:"error,omitempty"`
}

type Decoder struct{ reader *bufio.Reader }

func NewDecoder(reader io.Reader) *Decoder {
	return &Decoder{reader: bufio.NewReaderSize(reader, MaxFrameBytes+1)}
}

func (d *Decoder) Decode(value any) error {
	line, err := d.reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > MaxFrameBytes+1 {
		return ErrFrameTooLarge
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	line = bytes.TrimSuffix(line, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) == 0 || len(line) > MaxFrameBytes {
		if len(line) > MaxFrameBytes {
			return ErrFrameTooLarge
		}
		if errors.Is(err, io.EOF) {
			return io.EOF
		}
		return ErrInvalidFrame
	}
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return ErrInvalidFrame
	}
	return nil
}

func Encode(writer io.Writer, value any) error {
	frame, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(frame) > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	frame = append(frame, '\n')
	_, err = writer.Write(frame)
	return err
}

type Handler func(context.Context, Message) (json.RawMessage, *ProtocolError)

// Serve performs one handshake and one capability call, then returns.
func Serve(ctx context.Context, input io.Reader, output io.Writer, descriptor Descriptor, handler Handler) error {
	if descriptor.Validate() != nil || handler == nil {
		return ErrInvalidFrame
	}
	decoder := NewDecoder(input)
	var message Message
	if err := decoder.Decode(&message); err != nil {
		return err
	}
	if message.Type != "hello" || message.APIVersion != APIVersion {
		return Encode(output, Message{Type: "error", Error: &ProtocolError{Code: "unsupported_api"}})
	}
	if err := Encode(output, Message{Type: "hello", Descriptor: descriptor}); err != nil {
		return err
	}
	message = Message{}
	if err := decoder.Decode(&message); err != nil {
		return err
	}
	if message.Type != "call" || !validToken(message.ID, 64) || !message.Capability.Valid() || !validToken(message.Method, 64) || !json.Valid(message.Input) {
		return Encode(output, Message{Type: "error", ID: message.ID, Error: &ProtocolError{Code: "invalid_call"}})
	}
	if !descriptor.Has(message.Capability) {
		return Encode(output, Message{Type: "error", ID: message.ID, Error: &ProtocolError{Code: "capability_unavailable"}})
	}
	result, protocolError := handler(ctx, message)
	if protocolError != nil {
		if !protocolError.Valid() {
			protocolError = &ProtocolError{Code: "extension_failure"}
		}
		return Encode(output, Message{Type: "error", ID: message.ID, Error: protocolError})
	}
	if !json.Valid(result) {
		return Encode(output, Message{Type: "error", ID: message.ID, Error: &ProtocolError{Code: "invalid_result"}})
	}
	return Encode(output, Message{Type: "result", ID: message.ID, Output: result})
}

func validToken(value string, maximum int) bool {
	if value == "" || len(value) > maximum || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._", r) {
			continue
		}
		return false
	}
	return true
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("extension error: %s", e.Code)
}
