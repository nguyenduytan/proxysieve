package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/buildinfo"
	"github.com/nguyenduytan/proxysieve/internal/configload"
	"github.com/nguyenduytan/proxysieve/pkg/config"
	"go.yaml.in/yaml/v3"
)

const (
	portableFormatVersion = 1
	maxPortableArchive    = 8 << 20
	manifestName          = "manifest.json"
	configName            = "config.json"
)

var errPortable = errors.New("invalid portable export")

type portableManifest struct {
	FormatVersion int      `json:"format_version"`
	Product       string   `json:"product"`
	CreatedAt     string   `json:"created_at"`
	Files         []string `json:"files"`
}

type portableConfig struct {
	Manifest portableManifest `json:"manifest"`
	Config   config.Config    `json:"config"`
}

func runExport(args []string, stdout, stderr io.Writer, home string, env map[string]string) int {
	if len(args) != 0 && args[0] == "import" {
		return runImport(args[1:], stdout, stderr)
	}
	if len(args) != 0 && args[0] == "export" {
		args = args[1:]
	}
	f := newPortableFlagSet("export")
	file := f.String("file", "", "Explicit YAML configuration file")
	path := f.String("path", "", "Portable export archive path")
	if f.Parse(args) != nil || f.NArg() != 0 || *path == "" {
		return usageError(stderr)
	}
	effective, err := loadEffectiveConfig(*file, home, env)
	if err != nil {
		_, _ = io.WriteString(stderr, "CONFIG_INVALID\n")
		return 1
	}
	if err = writePortableExport(*path, effective.Config); err != nil {
		_, _ = fmt.Fprintf(stderr, "EXPORT_FAILED: %s\n", portableError(err))
		return 1
	}
	_, err = fmt.Fprintf(stdout, "export complete: %s\n", *path)
	if err != nil {
		return 1
	}
	return 0
}

func runImport(args []string, stdout, stderr io.Writer) int {
	f := newPortableFlagSet("import")
	path := f.String("path", "", "Portable export archive path")
	output := f.String("output", "", "Destination configuration file")
	dryRun := f.Bool("dry-run", false, "Validate and report without writing a file")
	if f.Parse(args) != nil || f.NArg() != 0 || *path == "" || (!*dryRun && *output == "") || (*dryRun && *output != "") {
		return usageError(stderr)
	}
	document, err := readPortableExport(*path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "IMPORT_FAILED: %s\n", portableError(err))
		return 1
	}
	if *dryRun {
		_, err = fmt.Fprintf(stdout, "portable export is valid: format %d, created %s\n", document.Manifest.FormatVersion, document.Manifest.CreatedAt)
		if err != nil {
			return 1
		}
		return 0
	}
	if err = writeImportedConfig(*output, document.Config); err != nil {
		_, _ = fmt.Fprintf(stderr, "IMPORT_FAILED: %s\n", portableError(err))
		return 1
	}
	_, err = fmt.Fprintf(stdout, "import complete: %s\n", *output)
	if err != nil {
		return 1
	}
	return 0
}

func newPortableFlagSet(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}

func writePortableExport(path string, value config.Config) error {
	if value.Validate() != nil {
		return errPortable
	}
	destination, err := filepath.Abs(path)
	if err != nil || strings.TrimSpace(path) == "" {
		return errPortable
	}
	manifest := portableManifest{FormatVersion: portableFormatVersion, Product: buildinfo.Current().Name, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Files: []string{manifestName, configName}}
	document := portableConfig{Manifest: manifest, Config: value.Clone()}
	b, err := json.MarshalIndent(document, "", "  ")
	if err != nil || len(b) > maxPortableArchive {
		return errPortable
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create(manifestName)
	if err != nil {
		return errPortable
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return errPortable
	}
	if _, err = entry.Write(manifestBytes); err != nil {
		return errPortable
	}
	entry, err = writer.Create(configName)
	if err != nil {
		return errPortable
	}
	if _, err = entry.Write(b); err != nil {
		return errPortable
	}
	if err = writer.Close(); err != nil || archive.Len() > maxPortableArchive {
		return errPortable
	}
	return writeNewFile(destination, archive.Bytes(), 0600)
}

func readPortableExport(path string) (portableConfig, error) {
	input, err := os.Open(path)
	if err != nil {
		return portableConfig{}, errPortable
	}
	defer func() { _ = input.Close() }()
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxPortableArchive {
		return portableConfig{}, errPortable
	}
	b, err := io.ReadAll(io.LimitReader(input, maxPortableArchive+1))
	if err != nil || len(b) > maxPortableArchive {
		return portableConfig{}, errPortable
	}
	archive, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(archive.File) != 2 {
		return portableConfig{}, errPortable
	}
	files := map[string][]byte{}
	var totalUncompressed uint64
	for _, entry := range archive.File {
		if entry.Name != manifestName && entry.Name != configName || entry.FileInfo().IsDir() || entry.UncompressedSize64 > maxPortableArchive {
			return portableConfig{}, errPortable
		}
		if entry.UncompressedSize64 > uint64(maxPortableArchive)-totalUncompressed {
			return portableConfig{}, errPortable
		}
		totalUncompressed += entry.UncompressedSize64
		if _, exists := files[entry.Name]; exists {
			return portableConfig{}, errPortable
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			return portableConfig{}, errPortable
		}
		content, readErr := io.ReadAll(io.LimitReader(reader, maxPortableArchive+1))
		_ = reader.Close()
		if readErr != nil || len(content) > maxPortableArchive {
			return portableConfig{}, errPortable
		}
		files[entry.Name] = content
	}
	var manifest portableManifest
	decoder := json.NewDecoder(bytes.NewReader(files[manifestName]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&manifest) != nil || manifest.FormatVersion != portableFormatVersion || manifest.Product != buildinfo.Current().Name || len(manifest.Files) != 2 || manifest.Files[0] != manifestName || manifest.Files[1] != configName {
		return portableConfig{}, errPortable
	}
	var document portableConfig
	decoder = json.NewDecoder(bytes.NewReader(files[configName]))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || !sameManifest(document.Manifest, manifest) || document.Config.Validate() != nil {
		return portableConfig{}, errPortable
	}
	return document, nil
}

func sameManifest(left, right portableManifest) bool {
	if left.FormatVersion != right.FormatVersion || left.Product != right.Product || left.CreatedAt != right.CreatedAt || len(left.Files) != len(right.Files) {
		return false
	}
	for i := range left.Files {
		if left.Files[i] != right.Files[i] {
			return false
		}
	}
	return true
}

func writeImportedConfig(path string, value config.Config) error {
	if value.Validate() != nil {
		return errPortable
	}
	b, err := yaml.Marshal(value)
	if err != nil || len(b) > configload.MaxDocumentBytes {
		return errPortable
	}
	return writeNewFile(path, b, 0600)
}

func writeNewFile(path string, content []byte, mode os.FileMode) error {
	if path == "" || len(content) == 0 {
		return errPortable
	}
	destination, err := filepath.Abs(path)
	if err != nil {
		return errPortable
	}
	parent := filepath.Dir(destination)
	if info, statErr := os.Stat(parent); statErr != nil || !info.IsDir() {
		return errPortable
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return errPortable
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(destination)
		}
	}()
	if _, err = file.Write(content); err != nil || file.Sync() != nil {
		return errPortable
	}
	keep = true
	return nil
}

func portableError(error) string { return "portable export was invalid, unsafe, or already exists" }
