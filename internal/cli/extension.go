package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	internale "github.com/nguyenduytan/proxysieve/internal/extension"
	publice "github.com/nguyenduytan/proxysieve/pkg/extension"
)

func runExtension(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "call" {
		return usageError(stderr)
	}
	flags := flag.NewFlagSet("extension call", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", "", "Trusted extension root")
	manifest := flags.String("manifest", "", "Manifest path relative to the extension root")
	capability := flags.String("capability", "", "Declared extension capability")
	method := flags.String("method", "", "Capability method")
	input := flags.String("input", "{}", "JSON input")
	timeout := flags.Duration("timeout", 5*time.Second, "Per-attempt timeout")
	attempts := flags.Int("attempts", 2, "Maximum process attempts")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *root == "" || *manifest == "" || *capability == "" || *method == "" || len(*input) > publice.MaxFrameBytes || !json.Valid([]byte(*input)) {
		return usageError(stderr)
	}
	response, err := internale.Run(context.Background(), internale.Request{
		Root: *root, Manifest: *manifest, Capability: publice.Capability(*capability), Method: *method,
		Input: json.RawMessage(*input), Timeout: *timeout, Attempts: *attempts,
	})
	if err != nil {
		var callError *internale.Error
		if errors.As(err, &callError) {
			_, _ = fmt.Fprintf(stderr, "EXTENSION_CALL_FAILED: %s (attempts=%d, stderr_bytes=%d, truncated=%t)\n", callError.Code, callError.Attempts, callError.StderrBytes, callError.StderrTruncated)
		} else {
			_, _ = io.WriteString(stderr, "EXTENSION_CALL_FAILED\n")
		}
		return 1
	}
	if _, err = stdout.Write(append(response.Output, '\n')); err != nil {
		return 1
	}
	return 0
}
