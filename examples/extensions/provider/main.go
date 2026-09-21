package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/nguyenduytan/proxysieve/pkg/extension"
)

func main() {
	descriptor := extension.Descriptor{Name: "example-provider", Version: "0.1.0", APIVersion: extension.APIVersion, Capabilities: []extension.Capability{extension.ProxyProvider}}
	err := extension.Serve(context.Background(), os.Stdin, os.Stdout, descriptor, func(_ context.Context, call extension.Message) (json.RawMessage, *extension.ProtocolError) {
		if call.Method != "fetch" {
			return nil, &extension.ProtocolError{Code: "method_unavailable"}
		}
		return json.RawMessage(`{"endpoints":[{"url":"http://127.0.0.1:8080","tags":["example"]}]}`), nil
	})
	if err != nil {
		os.Exit(1)
	}
}
