package extension

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	publicextension "github.com/nguyenduytan/proxysieve/pkg/extension"
)

const (
	maxManifestBytes = 64 << 10
	maxStderrBytes   = 64 << 10
)

type Request struct {
	Root, Manifest, Method string
	Capability             publicextension.Capability
	Input                  json.RawMessage
	Timeout                time.Duration
	Attempts               int
}

type Response struct {
	Descriptor publicextension.Descriptor
	Output     json.RawMessage
	Attempts   int
}

type Error struct {
	Code            string
	Attempts        int
	StderrBytes     int64
	StderrTruncated bool
	cause           error
}

func (e *Error) Error() string { return "extension call failed: " + e.Code }
func (e *Error) Unwrap() error { return e.cause }

type manifest struct {
	Name         string                       `json:"name"`
	Version      string                       `json:"version"`
	APIVersion   int                          `json:"api_version"`
	Command      string                       `json:"command"`
	Args         []string                     `json:"args,omitempty"`
	Capabilities []publicextension.Capability `json:"capabilities"`
	Permissions  []string                     `json:"permissions,omitempty"`
}

type loadedManifest struct {
	manifest
	path, command string
}

type attemptError struct {
	code  string
	retry bool
	err   error
}

func Run(ctx context.Context, request Request) (Response, error) {
	loaded, err := load(request.Root, request.Manifest)
	if err != nil {
		return Response{}, &Error{Code: "manifest_invalid", cause: err}
	}
	if !request.Capability.Valid() || !hasCapability(loaded.Capabilities, request.Capability) || !validToken(request.Method, 64) || !json.Valid(request.Input) || request.Timeout <= 0 || request.Timeout > 30*time.Second || request.Attempts < 1 || request.Attempts > 3 {
		return Response{}, &Error{Code: "request_invalid"}
	}
	var last *Error
	for attempt := 1; attempt <= request.Attempts; attempt++ {
		response, stderr, failure := runOnce(ctx, loaded, request)
		if failure == nil {
			response.Attempts = attempt
			return response, nil
		}
		last = &Error{Code: failure.code, Attempts: attempt, StderrBytes: stderr.total, StderrTruncated: stderr.total > maxStderrBytes, cause: failure.err}
		if !failure.retry {
			break
		}
	}
	return Response{}, last
}

func runOnce(ctx context.Context, loaded loadedManifest, request Request) (response Response, stderr *boundedWriter, failure *attemptError) {
	attemptContext, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()
	command := exec.CommandContext(attemptContext, loaded.command, loaded.Args...)
	command.Dir = filepath.Dir(loaded.path)
	command.Env = safeEnvironment()
	command.WaitDelay = time.Second
	stdin, err := command.StdinPipe()
	if err != nil {
		return response, &boundedWriter{}, &attemptError{code: "process_start", err: err}
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return response, &boundedWriter{}, &attemptError{code: "process_start", err: err}
	}
	stderr = &boundedWriter{}
	command.Stderr = stderr
	if err = command.Start(); err != nil {
		return response, stderr, &attemptError{code: "process_start", err: err}
	}
	waited := false
	defer func() {
		_ = stdin.Close()
		if !waited {
			cancel()
			_ = command.Wait()
		}
	}()
	decoder := publicextension.NewDecoder(stdout)
	if err = publicextension.Encode(stdin, publicextension.Message{Type: "hello", Descriptor: publicextension.Descriptor{APIVersion: publicextension.APIVersion}}); err != nil {
		return response, stderr, processFailure(attemptContext, "handshake_failed", err)
	}
	var message publicextension.Message
	if err = decoder.Decode(&message); err != nil {
		return response, stderr, processFailure(attemptContext, "handshake_failed", err)
	}
	if message.Type == "error" && message.Error != nil && message.Error.Valid() {
		return response, stderr, &attemptError{code: message.Error.Code}
	}
	descriptor := message.Descriptor
	if message.Type != "hello" || descriptor.Validate() != nil || descriptor.Name != loaded.Name || descriptor.Version != loaded.Version || descriptor.APIVersion != loaded.APIVersion {
		return response, stderr, &attemptError{code: "handshake_invalid", err: publicextension.ErrInvalidFrame}
	}
	if !descriptor.Has(request.Capability) {
		return response, stderr, &attemptError{code: "capability_mismatch"}
	}
	callID := "1"
	if err = publicextension.Encode(stdin, publicextension.Message{Type: "call", ID: callID, Capability: request.Capability, Method: request.Method, Input: request.Input}); err != nil {
		return response, stderr, processFailure(attemptContext, "call_failed", err)
	}
	message = publicextension.Message{}
	if err = decoder.Decode(&message); err != nil {
		return response, stderr, processFailure(attemptContext, "call_failed", err)
	}
	if message.Type == "error" && message.ID == callID && message.Error.Valid() {
		return response, stderr, &attemptError{code: message.Error.Code}
	}
	if message.Type != "result" || message.ID != callID || !json.Valid(message.Output) {
		return response, stderr, &attemptError{code: "result_invalid", err: publicextension.ErrInvalidFrame}
	}
	_ = stdin.Close()
	var extra [1]byte
	if count, readErr := stdout.Read(extra[:]); count != 0 || readErr != nil && !errors.Is(readErr, io.EOF) {
		return response, stderr, processFailure(attemptContext, "protocol_trailing_data", readErr)
	}
	err = command.Wait()
	waited = true
	if err != nil {
		return response, stderr, processFailure(attemptContext, "process_exit", err)
	}
	return Response{Descriptor: descriptor, Output: message.Output}, stderr, nil
}

func processFailure(ctx context.Context, code string, err error) *attemptError {
	if errors.Is(ctx.Err(), context.Canceled) {
		return &attemptError{code: "cancelled", err: ctx.Err()}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &attemptError{code: "timeout", retry: true, err: ctx.Err()}
	}
	if errors.Is(err, publicextension.ErrFrameTooLarge) {
		return &attemptError{code: "frame_too_large", err: err}
	}
	return &attemptError{code: code, retry: code == "handshake_failed" || code == "call_failed" || code == "process_exit", err: err}
}

func load(root, relative string) (loadedManifest, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return loadedManifest{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil || filepath.IsAbs(relative) || relative == "" {
		return loadedManifest{}, errors.New("invalid extension root or manifest path")
	}
	path, err := confined(root, filepath.Join(root, relative))
	if err != nil {
		return loadedManifest{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return loadedManifest{}, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxManifestBytes {
		return loadedManifest{}, errors.New("invalid extension manifest file")
	}
	var value manifest
	decoder := json.NewDecoder(io.LimitReader(file, maxManifestBytes+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return loadedManifest{}, errors.New("invalid extension manifest")
	}
	descriptor := publicextension.Descriptor{Name: value.Name, Version: value.Version, APIVersion: value.APIVersion, Capabilities: value.Capabilities}
	if descriptor.Validate() != nil || filepath.IsAbs(value.Command) || value.Command == "" || len(value.Args) > 32 || len(value.Permissions) > 16 {
		return loadedManifest{}, errors.New("invalid extension manifest values")
	}
	for _, arg := range value.Args {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return loadedManifest{}, errors.New("invalid extension argument")
		}
	}
	seen := map[string]bool{}
	for _, permission := range value.Permissions {
		if !validToken(permission, 64) || seen[permission] {
			return loadedManifest{}, errors.New("invalid extension permission")
		}
		seen[permission] = true
	}
	commandPath := filepath.Join(filepath.Dir(path), value.Command)
	if runtime.GOOS == "windows" && filepath.Ext(commandPath) == "" {
		if _, statErr := os.Stat(commandPath + ".exe"); statErr == nil {
			commandPath += ".exe"
		}
	}
	commandPath, err = confined(root, commandPath)
	if err != nil {
		return loadedManifest{}, err
	}
	info, err = os.Stat(commandPath)
	if err != nil || !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode()&0111 == 0 {
		return loadedManifest{}, errors.New("extension command is not executable")
	}
	return loadedManifest{manifest: value, path: path, command: commandPath}, nil
}

func confined(root, path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("extension path escapes its root")
	}
	return resolved, nil
}

func safeEnvironment() []string {
	environment := []string{"PROXYSIEVE_EXTENSION_API=1"}
	for _, name := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP", "TMPDIR", "LANG", "LC_ALL", "TZ"} {
		if value, ok := os.LookupEnv(name); ok && !strings.ContainsRune(value, 0) {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

func hasCapability(capabilities []publicextension.Capability, wanted publicextension.Capability) bool {
	for _, capability := range capabilities {
		if capability == wanted {
			return true
		}
	}
	return false
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

type boundedWriter struct{ total int64 }

func (writer *boundedWriter) Write(value []byte) (int, error) {
	writer.total += int64(len(value))
	return len(value), nil
}
