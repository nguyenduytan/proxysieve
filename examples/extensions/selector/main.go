package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/nguyenduytan/proxysieve/pkg/extension"
)

type selectionInput struct {
	Candidates []struct {
		ID          string `json:"id"`
		HealthScore int    `json:"health_score"`
	} `json:"candidates"`
}

func main() {
	descriptor := extension.Descriptor{Name: "example-selector", Version: "0.1.0", APIVersion: extension.APIVersion, Capabilities: []extension.Capability{extension.Selector}}
	err := extension.Serve(context.Background(), os.Stdin, os.Stdout, descriptor, func(_ context.Context, call extension.Message) (json.RawMessage, *extension.ProtocolError) {
		var input selectionInput
		if call.Method != "select" || json.Unmarshal(call.Input, &input) != nil || len(input.Candidates) == 0 || len(input.Candidates) > 1000 {
			return nil, &extension.ProtocolError{Code: "invalid_input"}
		}
		selected := input.Candidates[0]
		for _, candidate := range input.Candidates {
			if candidate.ID == "" || len(candidate.ID) > 128 || candidate.HealthScore < 0 || candidate.HealthScore > 100 {
				return nil, &extension.ProtocolError{Code: "invalid_input"}
			}
			if candidate.HealthScore > selected.HealthScore {
				selected = candidate
			}
		}
		result, _ := json.Marshal(map[string]string{"proxy_id": selected.ID})
		return result, nil
	})
	if err != nil {
		os.Exit(1)
	}
}
