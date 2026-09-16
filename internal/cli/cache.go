package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"

	"github.com/nguyenduytan/proxysieve/pkg/proxy"
)

type cacheCLIStats struct {
	Entries     int     `json:"entries"`
	Bytes       int64   `json:"bytes_stored"`
	MaxEntries  int     `json:"max_entries"`
	MaxBytes    int64   `json:"max_bytes"`
	Hits        uint64  `json:"hits"`
	Misses      uint64  `json:"misses"`
	Bypasses    uint64  `json:"bypasses"`
	Expired     uint64  `json:"expired"`
	Evictions   uint64  `json:"evictions"`
	BytesServed uint64  `json:"bytes_served"`
	HitRatio    float64 `json:"hit_ratio"`
}

type cacheCLIStatus struct {
	Enabled bool           `json:"enabled"`
	Stats   *cacheCLIStats `json:"stats,omitempty"`
}

type cacheCLIPurge struct {
	Domain string `json:"domain,omitempty"`
	Purged struct {
		Entries int   `json:"entries"`
		Bytes   int64 `json:"bytes"`
	} `json:"purged"`
	Stats cacheCLIStats `json:"stats"`
}

func runCache(args []string, stdout, stderr io.Writer, env map[string]string) int {
	if len(args) == 0 || args[0] != "stats" && args[0] != "purge" && args[0] != "purge-domain" {
		return usageError(stderr)
	}
	command := args[0]
	f := flag.NewFlagSet("cache "+command, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	adminURL := f.String("admin", "http://127.0.0.1:9090", "Admin API base URL")
	username := f.String("username", "", "Admin username")
	domain := f.String("domain", "", "Exact hostname to purge")
	jsonOutput := f.Bool("json", false, "Output machine-readable JSON")
	if f.Parse(args[1:]) != nil || *username == "" || command == "purge-domain" && *domain == "" || command != "purge-domain" && *domain != "" || f.NArg() != 0 {
		return usageError(stderr)
	}
	password := env[adminCLIPasswordEnv]
	if password == "" || len(password) > 4096 {
		_, _ = io.WriteString(stderr, "CACHE_AUTH_UNAVAILABLE: set PSV_ADMIN_PASSWORD for this command.\n")
		return 1
	}
	client, err := newAdminCLIClient(*adminURL)
	if err != nil || client.login(context.Background(), *username, password) != nil {
		_, _ = io.WriteString(stderr, "CACHE_AUTH_FAILED: verify the local Admin URL and credentials.\n")
		return 1
	}
	defer client.logout(context.Background())
	method, path, body := http.MethodGet, "/api/v1/cache/stats", []byte(nil)
	switch command {
	case "purge":
		method, path, body = http.MethodPost, "/api/v1/cache/purge", []byte("{}")
	case "purge-domain":
		method, path = http.MethodPost, "/api/v1/cache/purge/domain"
		body, _ = json.Marshal(map[string]string{"domain": *domain})
	}
	response, err := client.request(context.Background(), method, path, body, command != "stats")
	if err != nil || response.StatusCode != http.StatusOK {
		if response != nil {
			_ = response.Body.Close()
		}
		_, _ = io.WriteString(stderr, "CACHE_COMMAND_FAILED: the requested cache operation was not completed.\n")
		return 1
	}
	result, err := readBounded(response)
	if err != nil || validateCachePayload(command, result) != nil {
		_, _ = io.WriteString(stderr, "CACHE_COMMAND_FAILED: the Admin API returned invalid cache data.\n")
		return 1
	}
	if *jsonOutput {
		var pretty bytes.Buffer
		if json.Indent(&pretty, result, "", "  ") != nil || pretty.WriteByte('\n') != nil {
			return 1
		}
		if _, err = stdout.Write(pretty.Bytes()); err != nil {
			return 1
		}
		return 0
	}
	if command != "stats" {
		var value cacheCLIPurge
		_ = json.Unmarshal(result, &value)
		if command == "purge-domain" {
			_, err = fmt.Fprintf(stdout, "purged %d cache entries for %s (%d bytes)\n", value.Purged.Entries, value.Domain, value.Purged.Bytes)
		} else {
			_, err = fmt.Fprintf(stdout, "purged %d cache entries (%d bytes)\n", value.Purged.Entries, value.Purged.Bytes)
		}
		if err != nil {
			return 1
		}
		return 0
	}
	var value cacheCLIStatus
	_ = json.Unmarshal(result, &value)
	if !value.Enabled {
		_, err = io.WriteString(stdout, "Response cache disabled.\n")
	} else {
		_, err = fmt.Fprintf(stdout, "cache: %d/%d entries, %d/%d bytes, %.1f%% hit ratio\n", value.Stats.Entries, value.Stats.MaxEntries, value.Stats.Bytes, value.Stats.MaxBytes, value.Stats.HitRatio*100)
	}
	if err != nil {
		return 1
	}
	return 0
}

func validateCachePayload(command string, body []byte) error {
	if command == "stats" {
		document, ok := requiredJSONFields(body, "enabled")
		stats, hasStats := document["stats"]
		var value cacheCLIStatus
		if !ok || json.Unmarshal(body, &value) != nil || value.Enabled && (!hasStats || value.Stats == nil || !validCacheStats(stats)) || !value.Enabled && hasStats {
			return errAdminCLI
		}
		return nil
	}
	required := []string{"purged", "stats"}
	if command == "purge-domain" {
		required = append(required, "domain")
	}
	document, ok := requiredJSONFields(body, required...)
	_, purgedOK := requiredJSONFields(document["purged"], "entries", "bytes")
	var value cacheCLIPurge
	if !ok || !purgedOK || json.Unmarshal(body, &value) != nil || command == "purge-domain" && !proxy.ValidHost(value.Domain) || value.Purged.Entries < 0 || value.Purged.Bytes < 0 || !validCacheStats(document["stats"]) {
		return errAdminCLI
	}
	return nil
}

func validCacheStats(raw []byte) bool {
	_, ok := requiredJSONFields(raw, "entries", "bytes_stored", "max_entries", "max_bytes", "hits", "misses", "bypasses", "expired", "evictions", "bytes_served", "hit_ratio")
	var stats cacheCLIStats
	return ok && json.Unmarshal(raw, &stats) == nil && stats.Entries >= 0 && stats.Bytes >= 0 && stats.MaxEntries > 0 && stats.MaxBytes > 0 && stats.Entries <= stats.MaxEntries && stats.Bytes <= stats.MaxBytes && stats.HitRatio >= 0 && stats.HitRatio <= 1
}

func requiredJSONFields(raw []byte, names ...string) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, false
	}
	for _, name := range names {
		if _, ok := fields[name]; !ok {
			return nil, false
		}
	}
	return fields, true
}
