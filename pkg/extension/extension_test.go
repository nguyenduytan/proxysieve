package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestServe(t *testing.T) {
	input := strings.NewReader("{\"type\":\"hello\",\"api_version\":1}\n{\"type\":\"call\",\"id\":\"1\",\"capability\":\"selector\",\"method\":\"select\",\"input\":{}}\n")
	var output bytes.Buffer
	descriptor := Descriptor{Name: "test-selector", Version: "1.0.0", APIVersion: APIVersion, Capabilities: []Capability{Selector}}
	if err := Serve(t.Context(), input, &output, descriptor, func(_ context.Context, call Message) (json.RawMessage, *ProtocolError) {
		if call.Method != "select" {
			t.Fatal("unexpected method")
		}
		return json.RawMessage(`{"proxy_id":"one"}`), nil
	}); err != nil {
		t.Fatal(err)
	}
	decoder := NewDecoder(&output)
	var hello, result Message
	if err := decoder.Decode(&hello); err != nil || hello.Type != "hello" || hello.Name != descriptor.Name {
		t.Fatalf("invalid hello: %+v %v", hello, err)
	}
	if err := decoder.Decode(&result); err != nil || result.Type != "result" || string(result.Output) != `{"proxy_id":"one"}` {
		t.Fatalf("invalid result: %+v %v", result, err)
	}
}

func TestDecoderBoundsAndSchema(t *testing.T) {
	if err := NewDecoder(strings.NewReader(strings.Repeat("x", MaxFrameBytes+1) + "\n")).Decode(&Message{}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized frame error = %v", err)
	}
	if err := NewDecoder(strings.NewReader(`{"type":"hello","unknown":true}`)).Decode(&Message{}); !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("unknown field error = %v", err)
	}
}

func FuzzDecode(f *testing.F) {
	f.Add([]byte("{\"type\":\"hello\",\"api_version\":1}\n"))
	f.Add([]byte("not json\n"))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > MaxFrameBytes+2 {
			return
		}
		var message Message
		_ = NewDecoder(bytes.NewReader(input)).Decode(&message)
	})
}
