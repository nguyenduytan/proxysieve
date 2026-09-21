package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/nguyenduytan/proxysieve/pkg/extension"
)

func main() {
	descriptor := extension.Descriptor{Name: "example-alert-sink", Version: "0.1.0", APIVersion: extension.APIVersion, Capabilities: []extension.Capability{extension.AlertSink}}
	err := extension.Serve(context.Background(), os.Stdin, os.Stdout, descriptor, func(_ context.Context, call extension.Message) (json.RawMessage, *extension.ProtocolError) {
		var input struct {
			Type string `json:"type"`
		}
		if call.Method != "deliver" || json.Unmarshal(call.Input, &input) != nil || strings.TrimSpace(input.Type) == "" || len(input.Type) > 128 {
			return nil, &extension.ProtocolError{Code: "invalid_input"}
		}
		return json.RawMessage(`{"accepted":true}`), nil
	})
	if err != nil {
		os.Exit(1)
	}
}
