// Package configload owns bounded YAML decoding and configuration precedence.
package configload

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/config"
	"go.yaml.in/yaml/v3"
)

const MaxDocumentBytes = 1 << 20

type Options struct {
	Home    string
	File    io.Reader // nil means defaults, not an implicit read from the working directory.
	Env     map[string]string
	Flags   map[string]string // Canonical dotted paths, not raw command-line arguments.
	Runtime map[string]string
}
type Effective struct {
	Config  config.Config     `json:"config"`
	Sources map[string]string `json:"sources"`
}

var envPaths = map[string]string{
	"PROXYSIEVE_LOG_LEVEL":      "logging.level",
	"PROXYSIEVE_LOG_FORMAT":     "logging.format",
	"PROXYSIEVE_DATA_DIR":       "server.data_dir",
	"PROXYSIEVE_ADMIN_BIND":     "admin.bind",
	"PROXYSIEVE_API_BIND":       "admin.bind",
	"PROXYSIEVE_STORAGE_DRIVER": "storage.driver",
	"PROXYSIEVE_STORAGE_PATH":   "storage.path",
}

func Load(o Options) (Effective, error) {
	c := config.Defaults(o.Home)
	sources := map[string]string{}
	raw, _ := json.Marshal(c)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	markLeaves(fields, "", "default", sources)
	if o.File != nil {
		b, err := io.ReadAll(io.LimitReader(o.File, MaxDocumentBytes+1))
		if err != nil || len(b) > MaxDocumentBytes {
			return Effective{}, config.ErrInvalid
		}
		var tree yaml.Node
		d := yaml.NewDecoder(bytes.NewReader(b))
		if d.Decode(&tree) != nil {
			return Effective{}, config.ErrInvalid
		}
		var extra yaml.Node
		if !errors.Is(d.Decode(&extra), io.EOF) || len(tree.Content) != 1 || tree.Content[0].Kind != yaml.MappingNode {
			return Effective{}, config.ErrInvalid
		}
		count := 0
		if !safeNode(&tree, 0, &count) {
			return Effective{}, config.ErrInvalid
		}
		root := tree.Content[0]
		version := false
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == "version" {
				version = true
			}
		}
		if !version {
			return Effective{}, config.ErrInvalid
		}
		d = yaml.NewDecoder(bytes.NewReader(b))
		d.KnownFields(true)
		if d.Decode(&c) != nil {
			return Effective{}, config.ErrInvalid
		}
		markNode(root, "", sources)
		// Arrays replace in full. Default only missing fields, never explicit zero.
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value != "listeners" {
				continue
			}
			for index, node := range root.Content[i+1].Content {
				if index >= len(c.Listeners) {
					return Effective{}, config.ErrInvalid
				}
				present := map[string]bool{}
				for j := 0; j < len(node.Content); j += 2 {
					present[node.Content[j].Value] = true
				}
				if !present["max_connections"] {
					c.Listeners[index].MaxConnections = 256
				}
				if !present["idle_timeout"] {
					c.Listeners[index].IdleTimeout = config.Duration(2 * time.Minute)
				}
			}
		}
	}
	raw, _ = json.Marshal(c)
	_ = json.Unmarshal(raw, &fields)
	envValues := map[string]string{}
	keys := make([]string, 0, len(o.Env))
	for k := range o.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.HasPrefix(k, "PROXYSIEVE_SECRET_") {
			continue
		}
		if !strings.HasPrefix(k, "PROXYSIEVE_") {
			continue
		}
		path, ok := envPaths[k]
		if !ok {
			return Effective{}, config.ErrInvalid
		}
		if prev, exists := envValues[path]; exists && prev != o.Env[k] {
			return Effective{}, config.ErrInvalid
		}
		envValues[path] = o.Env[k]
	}
	for _, layer := range []struct {
		name   string
		values map[string]string
	}{{"environment", envValues}, {"flags", o.Flags}, {"runtime", o.Runtime}} {
		for path, v := range layer.values {
			if !set(fields, path, v) {
				return Effective{}, config.ErrInvalid
			}
			sources[path] = layer.name
		}
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return Effective{}, config.ErrInvalid
	}
	if json.Unmarshal(raw, &c) != nil || c.Validate() != nil {
		return Effective{}, config.ErrInvalid
	}
	if sources["storage.path"] == "default" && sources["server.data_dir"] != "default" {
		c.Storage.Path = filepath.Join(c.Server.DataDir, "proxysieve.db")
		sources["storage.path"] = "derived:server.data_dir"
	}
	return Effective{Config: c, Sources: sources}, nil
}

// Reject aliases, nulls, merge keys, duplicate keys and excessive nesting up front.
func safeNode(n *yaml.Node, depth int, count *int) bool {
	*count++
	if depth > 24 || *count > 10_000 || n.Kind == yaml.AliasNode || n.Tag == "!!null" || n.Tag == "!!merge" {
		return false
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
				return false
			}
			seen[k.Value] = true
		}
	}
	for _, child := range n.Content {
		if !safeNode(child, depth+1, count) {
			return false
		}
	}
	return true
}

func set(m map[string]any, path, value string) bool {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := m[p].(map[string]any)
		if !ok {
			return false
		}
		m = next
	}
	k := parts[len(parts)-1]
	switch m[k].(type) {
	case string:
		m[k] = value
	case bool:
		v, err := strconv.ParseBool(value)
		if err != nil {
			return false
		}
		m[k] = v
	case float64, int64, json.Number:
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return false
		}
		m[k] = json.Number(strconv.FormatInt(v, 10))
	default:
		return false
	}
	return true
}
func markLeaves(m map[string]any, prefix, source string, out map[string]string) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if nested, ok := v.(map[string]any); ok {
			markLeaves(nested, path, source, out)
		} else {
			out[path] = source
		}
	}
}
func markNode(n *yaml.Node, prefix string, out map[string]string) {
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		path := k.Value
		if prefix != "" {
			path = prefix + "." + path
		}
		if v.Kind == yaml.MappingNode {
			markNode(v, path, out)
		} else {
			out[path] = "file"
		}
	}
}
