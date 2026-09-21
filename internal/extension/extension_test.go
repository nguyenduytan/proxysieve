package extension

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	publicextension "github.com/nguyenduytan/proxysieve/pkg/extension"
)

func TestRunNegotiatesAndCallsCapabilities(t *testing.T) {
	root, manifest := testExtension(t, "success", []publicextension.Capability{publicextension.ProxyProvider, publicextension.Selector, publicextension.AlertSink})
	for _, test := range []struct {
		capability publicextension.Capability
		method     string
	}{
		{publicextension.ProxyProvider, "fetch"},
		{publicextension.Selector, "select"},
		{publicextension.AlertSink, "deliver"},
	} {
		response, err := Run(t.Context(), Request{Root: root, Manifest: manifest, Capability: test.capability, Method: test.method, Input: json.RawMessage(`{}`), Timeout: 2 * time.Second, Attempts: 1})
		if err != nil || response.Attempts != 1 || string(response.Output) != `{"ok":true}` || response.Descriptor.APIVersion != publicextension.APIVersion {
			t.Fatalf("%s response=%+v error=%v", test.capability, response, err)
		}
	}
}

func TestRunRejectsHandshakeAndCapabilityMismatch(t *testing.T) {
	for _, test := range []struct {
		mode string
		code string
	}{
		{"api-mismatch", "handshake_invalid"},
		{"capability-mismatch", "capability_mismatch"},
	} {
		root, manifest := testExtension(t, test.mode, []publicextension.Capability{publicextension.ProxyProvider})
		_, err := Run(t.Context(), Request{Root: root, Manifest: manifest, Capability: publicextension.ProxyProvider, Method: "fetch", Input: json.RawMessage(`{}`), Timeout: 2 * time.Second, Attempts: 1})
		assertCallError(t, err, test.code, 1)
	}
}

func TestRunContainsPaths(t *testing.T) {
	root, _ := testExtension(t, "success", []publicextension.Capability{publicextension.ProxyProvider})
	request := Request{Root: root, Manifest: filepath.Join("..", "manifest.json"), Capability: publicextension.ProxyProvider, Method: "fetch", Input: json.RawMessage(`{}`), Timeout: time.Second, Attempts: 1}
	_, err := Run(t.Context(), request)
	assertCallError(t, err, "manifest_invalid", 0)

	outside := t.TempDir()
	executable := filepath.Join(outside, "outside")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	copyFile(t, os.Args[0], executable)
	link := filepath.Join(root, "outside")
	if runtime.GOOS == "windows" {
		link += ".exe"
	}
	if err = os.Symlink(executable, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	writeManifest(t, root, "symlink.json", "./outside", "success", []publicextension.Capability{publicextension.ProxyProvider})
	request.Manifest = "symlink.json"
	_, err = Run(t.Context(), request)
	assertCallError(t, err, "manifest_invalid", 0)
}

func TestRunBoundsFramesStderrTimeoutAndRestarts(t *testing.T) {
	for _, test := range []struct {
		mode, code                 string
		attempts, expectedAttempts int
		timeout                    time.Duration
	}{
		{"oversize", "frame_too_large", 3, 1, time.Second},
		{"crash", "handshake_failed", 3, 3, time.Second},
		{"hang", "timeout", 2, 2, 50 * time.Millisecond},
		{"stderr", "handshake_failed", 2, 2, time.Second},
	} {
		root, manifest := testExtension(t, test.mode, []publicextension.Capability{publicextension.ProxyProvider})
		_, err := Run(t.Context(), Request{Root: root, Manifest: manifest, Capability: publicextension.ProxyProvider, Method: "fetch", Input: json.RawMessage(`{}`), Timeout: test.timeout, Attempts: test.attempts})
		callError := assertCallError(t, err, test.code, test.expectedAttempts)
		if test.mode == "stderr" && (callError.StderrBytes <= maxStderrBytes || !callError.StderrTruncated) {
			t.Fatalf("stderr was not bounded: %+v", callError)
		}
	}
}

func TestRunReturnsStructuredExtensionErrorWithoutRestart(t *testing.T) {
	root, manifest := testExtension(t, "structured-error", []publicextension.Capability{publicextension.ProxyProvider})
	_, err := Run(t.Context(), Request{Root: root, Manifest: manifest, Capability: publicextension.ProxyProvider, Method: "fetch", Input: json.RawMessage(`{}`), Timeout: time.Second, Attempts: 3})
	assertCallError(t, err, "invalid_input", 1)
}

func TestRunCancellationDoesNotRestart(t *testing.T) {
	root, manifest := testExtension(t, "hang", []publicextension.Capability{publicextension.ProxyProvider})
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := Run(ctx, Request{Root: root, Manifest: manifest, Capability: publicextension.ProxyProvider, Method: "fetch", Input: json.RawMessage(`{}`), Timeout: time.Second, Attempts: 3})
	assertCallError(t, err, "cancelled", 1)
}

func TestRunRejectsInvalidManifestDocuments(t *testing.T) {
	root, manifest := testExtension(t, "success", []publicextension.Capability{publicextension.ProxyProvider})
	request := Request{Root: root, Manifest: manifest, Capability: publicextension.ProxyProvider, Method: "fetch", Input: json.RawMessage(`{}`), Timeout: time.Second, Attempts: 1}
	for _, content := range [][]byte{
		[]byte(`{"name":"test-extension","unknown":true}`),
		[]byte(strings.Repeat("x", maxManifestBytes+1)),
	} {
		if err := os.WriteFile(filepath.Join(root, manifest), content, 0600); err != nil {
			t.Fatal(err)
		}
		_, err := Run(t.Context(), request)
		assertCallError(t, err, "manifest_invalid", 0)
	}
}

func TestExtensionHelperProcess(t *testing.T) {
	mode := ""
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) {
			mode = os.Args[index+1]
			break
		}
	}
	if mode == "" {
		return
	}
	switch mode {
	case "crash":
		os.Exit(3)
	case "stderr":
		_, _ = io.WriteString(os.Stderr, strings.Repeat("x", maxStderrBytes+1024))
		os.Exit(3)
	case "hang":
		time.Sleep(time.Hour)
	case "oversize":
		var hello publicextension.Message
		_ = publicextension.NewDecoder(os.Stdin).Decode(&hello)
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", publicextension.MaxFrameBytes+1)+"\n")
		os.Exit(0)
	case "api-mismatch":
		var hello publicextension.Message
		_ = publicextension.NewDecoder(os.Stdin).Decode(&hello)
		_ = publicextension.Encode(os.Stdout, publicextension.Message{Type: "hello", Descriptor: publicextension.Descriptor{Name: "test-extension", Version: "0.1.0", APIVersion: 2, Capabilities: []publicextension.Capability{publicextension.ProxyProvider}}})
		os.Exit(0)
	}
	capabilities := []publicextension.Capability{publicextension.ProxyProvider, publicextension.Selector, publicextension.AlertSink}
	if mode == "capability-mismatch" {
		capabilities = []publicextension.Capability{publicextension.Selector}
	}
	descriptor := publicextension.Descriptor{Name: "test-extension", Version: "0.1.0", APIVersion: publicextension.APIVersion, Capabilities: capabilities}
	err := publicextension.Serve(context.Background(), os.Stdin, os.Stdout, descriptor, func(_ context.Context, _ publicextension.Message) (json.RawMessage, *publicextension.ProtocolError) {
		if mode == "structured-error" {
			return nil, &publicextension.ProtocolError{Code: "invalid_input"}
		}
		return json.RawMessage(`{"ok":true}`), nil
	})
	if err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func testExtension(t *testing.T, mode string, capabilities []publicextension.Capability) (string, string) {
	t.Helper()
	root := t.TempDir()
	name := "helper"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	copyFile(t, os.Args[0], filepath.Join(root, name))
	writeManifest(t, root, "manifest.json", "./helper", mode, capabilities)
	return root, "manifest.json"
}

func writeManifest(t *testing.T, root, name, command, mode string, capabilities []publicextension.Capability) {
	t.Helper()
	value := manifest{Name: "test-extension", Version: "0.1.0", APIVersion: publicextension.APIVersion, Command: command, Args: []string{"-test.run=^TestExtensionHelperProcess$", "--", mode}, Capabilities: capabilities}
	content, err := json.Marshal(value)
	if err == nil {
		err = os.WriteFile(filepath.Join(root, name), content, 0600)
	}
	if err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func copyFile(t *testing.T, source, destination string) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err = output.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertCallError(t *testing.T, err error, code string, attempts int) *Error {
	t.Helper()
	var callError *Error
	if !errors.As(err, &callError) || callError.Code != code || callError.Attempts != attempts {
		t.Fatalf("error = %+v, want code %s attempts %d", err, code, attempts)
	}
	return callError
}
