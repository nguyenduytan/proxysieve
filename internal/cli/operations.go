package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/configload"
	"github.com/nguyenduytan/proxysieve/internal/storage/sqlite"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func runDB(args []string, stdout, stderr io.Writer, home string, env map[string]string) int {
	if len(args) == 0 || (args[0] != "status" && args[0] != "migrate" && args[0] != "compact") {
		return usageError(stderr)
	}
	f := flag.NewFlagSet("db "+args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	file := f.String("file", "", "Configuration file")
	jsonOutput := false
	if args[0] == "status" {
		f.BoolVar(&jsonOutput, "json", false, "Output machine-readable JSON")
	}
	if f.Parse(args[1:]) != nil || f.NArg() != 0 {
		return usageError(stderr)
	}
	effective, err := loadEffectiveConfig(*file, home, env)
	if err != nil {
		_, _ = io.WriteString(stderr, "CONFIG_INVALID\n")
		return 1
	}
	if effective.Config.Storage.Driver != "sqlite" {
		_, _ = io.WriteString(stderr, "STORAGE_UNSUPPORTED: database operations require SQLite.\n")
		return 1
	}
	repository, err := sqlite.Open(context.Background(), effective.Config.Storage.Path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "DB_%s_FAILED: %s\n", strings.ToUpper(args[0]), operationError(err))
		return 1
	}
	defer func() { _ = repository.Close() }()
	if args[0] == "compact" {
		if err = repository.Compact(context.Background()); err != nil {
			_, _ = fmt.Fprintf(stderr, "DB_COMPACT_FAILED: %s\n", operationError(err))
			return 1
		}
	}
	status, err := repository.Status(context.Background())
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "DB_%s_FAILED: %s\n", strings.ToUpper(args[0]), operationError(err))
		return 1
	}
	if args[0] == "status" && jsonOutput {
		if err = json.NewEncoder(stdout).Encode(status); err != nil {
			return 1
		}
		return 0
	}
	if args[0] == "status" {
		_, err = fmt.Fprintf(stdout, "schema=%d journal=%s endpoints=%d sources=%d pools=%d chains=%d policies=%d\n", status.SchemaVersion, status.JournalMode, status.EndpointCount, status.SourceCount, status.PoolCount, status.ChainCount, status.PolicyCount)
	} else {
		_, err = fmt.Fprintf(stdout, "database %s complete: schema %d\n", args[0], status.SchemaVersion)
	}
	if err != nil {
		return 1
	}
	return 0
}

func runOperations(args []string, stdout, stderr io.Writer, home string, env map[string]string) int {
	if len(args) == 0 || (args[0] != "backup" && args[0] != "restore") {
		return usageError(stderr)
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(io.Discard)
	file := f.String("file", "", "Configuration file")
	path := f.String("path", "", "Backup file path")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || *path == "" {
		return usageError(stderr)
	}
	effective, err := loadEffectiveConfig(*file, home, env)
	if err != nil {
		_, _ = io.WriteString(stderr, "CONFIG_INVALID\n")
		return 1
	}
	if effective.Config.Storage.Driver != "sqlite" {
		_, _ = io.WriteString(stderr, "STORAGE_UNSUPPORTED: backup and restore require SQLite.\n")
		return 1
	}
	ctx := context.Background()
	if args[0] == "backup" {
		err = backupDatabase(ctx, effective.Config.Storage.Path, *path)
	} else {
		err = restoreDatabase(ctx, effective.Config.Storage.Path, *path)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s_FAILED: %s\n", args[0], operationError(err))
		return 1
	}
	_, _ = fmt.Fprintf(stdout, "%s complete: %s\n", args[0], *path)
	return 0
}

func loadEffectiveConfig(file, home string, env map[string]string) (configload.Effective, error) {
	o := configload.Options{Home: home, Env: env}
	if file != "" {
		r, err := os.Open(file)
		if err != nil {
			return configload.Effective{}, err
		}
		defer func() { _ = r.Close() }()
		o.File = r
	}
	return configload.Load(o)
}

func backupDatabase(ctx context.Context, source, destination string) error {
	if source == "" || destination == "" {
		return store.ErrInvalid
	}
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return store.ErrInvalid
	}
	destinationAbs, err := filepath.Abs(destination)
	if err != nil || sourceAbs == destinationAbs {
		return store.ErrInvalid
	}
	if info, statErr := os.Stat(sourceAbs); statErr != nil || !info.Mode().IsRegular() {
		return store.ErrNotFound
	}
	repository, err := sqlite.Open(ctx, sourceAbs)
	if err != nil {
		return err
	}
	defer func() { _ = repository.Close() }()
	return repository.BackupTo(ctx, destinationAbs)
}

func restoreDatabase(ctx context.Context, destination, backup string) error {
	destinationAbs, err := filepath.Abs(destination)
	if err != nil {
		return store.ErrInvalid
	}
	backupAbs, err := filepath.Abs(backup)
	if err != nil || destinationAbs == backupAbs {
		return store.ErrInvalid
	}
	info, err := os.Stat(backupAbs)
	if err != nil || !info.Mode().IsRegular() {
		return store.ErrNotFound
	}
	if err = sqlite.ValidateBackup(ctx, backupAbs); err != nil {
		return err
	}
	parent := filepath.Dir(destinationAbs)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return store.ErrInvalid
	}
	tmp, err := os.CreateTemp(parent, ".proxysieve-restore-*")
	if err != nil {
		return store.ErrUnavailable
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err = tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return store.ErrUnavailable
	}
	if err = copyFile(backupAbs, tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return store.ErrUnavailable
	}
	if _, err = os.Stat(destinationAbs); err == nil {
		archive := fmt.Sprintf("%s.pre-restore-%s.db", destinationAbs, time.Now().UTC().Format("20060102T150405.000000000Z"))
		if err = os.Rename(destinationAbs, archive); err != nil {
			return store.ErrUnavailable
		}
		for _, sidecar := range []string{"-wal", "-shm"} {
			currentSidecar := destinationAbs + sidecar
			if _, sidecarErr := os.Stat(currentSidecar); sidecarErr == nil {
				if sidecarErr = os.Rename(currentSidecar, archive+sidecar); sidecarErr != nil {
					return store.ErrUnavailable
				}
			} else if !errors.Is(sidecarErr, os.ErrNotExist) {
				return store.ErrUnavailable
			}
		}
	}
	if err = os.Rename(tmpPath, destinationAbs); err != nil {
		return store.ErrUnavailable
	}
	return nil
}

func copyFile(source string, destination *os.File) error {
	input, err := os.Open(source)
	if err != nil {
		return store.ErrUnavailable
	}
	defer func() { _ = input.Close() }()
	if _, err = io.Copy(destination, input); err != nil {
		return store.ErrUnavailable
	}
	if err = destination.Sync(); err != nil {
		return store.ErrUnavailable
	}
	return nil
}

func operationError(err error) string {
	switch {
	case errors.Is(err, store.ErrInvalid):
		return "path or storage configuration was not accepted"
	case errors.Is(err, store.ErrConflict):
		return "destination already exists; choose a new path"
	case errors.Is(err, store.ErrNotFound):
		return "database or backup file was not found"
	case errors.Is(err, store.ErrSchema):
		return "backup schema is not supported"
	default:
		return "operation could not be completed"
	}
}
