package proxy

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/nguyenduytan/proxysieve/pkg/model"
)

type ImportFormat string

const (
	ImportText ImportFormat = "text"
	ImportCSV  ImportFormat = "csv"
	ImportJSON ImportFormat = "json"
)

type ImportMapping struct {
	ItemsField    string `json:"items_field,omitempty"`
	EndpointField string `json:"endpoint_field,omitempty"`
	ProtocolField string `json:"protocol_field,omitempty"`
	HostField     string `json:"host_field,omitempty"`
	PortField     string `json:"port_field,omitempty"`
}

type ImportOptions struct {
	Format  ImportFormat  `json:"format,omitempty"`
	Mapping ImportMapping `json:"mapping,omitempty"`
}

type DuplicateMode string

const (
	SkipDuplicates   DuplicateMode = "skip"
	UpdateDuplicates DuplicateMode = "update"
	CreateDuplicates DuplicateMode = "create"
)

type PreviewItem struct {
	Line   int         `json:"line"`
	Result ParseResult `json:"result"`
	Error  string      `json:"error,omitempty"`
}
type ImportPreview struct {
	Items      []PreviewItem    `json:"items"`
	Valid      int              `json:"valid"`
	Invalid    int              `json:"invalid"`
	Duplicates int              `json:"duplicates"`
	Protocols  map[Protocol]int `json:"protocols"`
}

// Preview parses a bounded text import without persisting anything. Lines are
// intentionally capped to prevent a single request consuming unbounded memory.
func Preview(input string, maxLines int) (ImportPreview, error) {
	return PreviewWithOptions(input, maxLines, ImportOptions{})
}

func PreviewWithOptions(input string, maxRecords int, options ImportOptions) (ImportPreview, error) {
	if maxRecords < 1 || maxRecords > 100_000 || len(input) > 8<<20 {
		return ImportPreview{}, ErrParse
	}
	items, err := importItems(input, maxRecords, options)
	if err != nil {
		return ImportPreview{}, err
	}
	out := ImportPreview{Protocols: map[Protocol]int{}}
	seen := map[string]bool{}
	for _, item := range items {
		if item.Error != "" {
			out.Invalid++
		} else {
			out.Valid++
			out.Protocols[item.Result.Endpoint.Protocol]++
			key := Identity(item.Result.Endpoint)
			if seen[key] {
				out.Duplicates++
			} else {
				seen[key] = true
			}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func importItems(input string, maxRecords int, options ImportOptions) ([]PreviewItem, error) {
	for _, field := range []string{options.Mapping.ItemsField, options.Mapping.EndpointField, options.Mapping.ProtocolField, options.Mapping.HostField, options.Mapping.PortField} {
		if len(field) > 256 || strings.ContainsRune(field, 0) {
			return nil, ErrParse
		}
	}
	format := options.Format
	if format == "" {
		format = ImportText
	}
	switch format {
	case ImportText:
		return textItems(input, maxRecords)
	case ImportCSV:
		return csvItems(input, maxRecords, options.Mapping)
	case ImportJSON:
		return jsonItems(input, maxRecords, options.Mapping)
	default:
		return nil, ErrParse
	}
}

func textItems(input string, maxRecords int) ([]PreviewItem, error) {
	items := make([]PreviewItem, 0)
	for line, raw := range strings.Split(input, "\n") {
		if line >= maxRecords {
			return nil, ErrParse
		}
		if raw = strings.TrimSpace(raw); raw != "" {
			items = append(items, previewItem(line+1, raw))
		}
	}
	return items, nil
}

func csvItems(input string, maxRecords int, mapping ImportMapping) ([]PreviewItem, error) {
	reader := csv.NewReader(strings.NewReader(input))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil || len(header) == 0 {
		return nil, ErrParse
	}
	fields := make(map[string]int, len(header))
	for i, raw := range header {
		name := strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
		if _, exists := fields[name]; name == "" || exists {
			return nil, ErrParse
		}
		fields[name] = i
	}
	items := make([]PreviewItem, 0)
	for record := 1; ; record++ {
		row, readErr := reader.Read()
		if readErr == io.EOF {
			return items, nil
		}
		if readErr != nil || record > maxRecords {
			return nil, ErrParse
		}
		if len(row) != len(header) {
			items = append(items, PreviewItem{Line: record, Error: ErrParse.Error()})
			continue
		}
		values := make(map[string]string, len(row))
		for name, position := range fields {
			if position < len(row) {
				values[name] = row[position]
			}
		}
		items = append(items, mappedItem(record, values, mapping))
	}
}

func jsonItems(input string, maxRecords int, mapping ImportMapping) ([]PreviewItem, error) {
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	var document any
	if decoder.Decode(&document) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return nil, ErrParse
	}
	if object, ok := document.(map[string]any); ok {
		field := mapping.ItemsField
		if field == "" {
			field = "items"
		}
		document = object[field]
	}
	records, ok := document.([]any)
	if !ok || len(records) > maxRecords {
		return nil, ErrParse
	}
	items := make([]PreviewItem, 0, len(records))
	for i, record := range records {
		switch value := record.(type) {
		case string:
			items = append(items, previewItem(i+1, value))
		case map[string]any:
			values := make(map[string]string, len(value))
			for name, raw := range value {
				switch typed := raw.(type) {
				case string:
					values[name] = typed
				case json.Number:
					values[name] = typed.String()
				}
			}
			items = append(items, mappedItem(i+1, values, mapping))
		default:
			items = append(items, PreviewItem{Line: i + 1, Error: ErrParse.Error()})
		}
	}
	return items, nil
}

func mappedItem(record int, values map[string]string, mapping ImportMapping) PreviewItem {
	endpointField, protocolField, hostField, portField := mapping.EndpointField, mapping.ProtocolField, mapping.HostField, mapping.PortField
	if endpointField == "" {
		endpointField = "endpoint"
	}
	if protocolField == "" {
		protocolField = "protocol"
	}
	if hostField == "" {
		hostField = "host"
	}
	if portField == "" {
		portField = "port"
	}
	if raw := strings.TrimSpace(values[endpointField]); raw != "" {
		return previewItem(record, raw)
	}
	protocol := strings.TrimSpace(values[protocolField])
	if protocol == "" {
		protocol = string(HTTP)
	}
	host, port := strings.TrimSpace(values[hostField]), strings.TrimSpace(values[portField])
	return previewItem(record, protocol+"://"+net.JoinHostPort(host, port))
}

func previewItem(line int, raw string) PreviewItem {
	result, err := Parse(strings.TrimSpace(raw))
	if err != nil {
		return PreviewItem{Line: line, Error: ErrParse.Error()}
	}
	return PreviewItem{Line: line, Result: result}
}

// Identity excludes credentials because Parse deliberately does not retain them.
// A later authenticated import must pass a SecretRef explicitly to persistence.
func Identity(e Endpoint) string {
	return string(e.Protocol) + "|" + strings.ToLower(e.Host) + "|" + strconv.Itoa(int(e.Port))
}

type ImportRequest struct {
	Input    string
	Mode     DuplicateMode
	Existing map[string]Endpoint
	Options  ImportOptions
}
type ImportResult struct {
	Endpoints []Endpoint
	Skipped   int
	Updated   int
	Created   int
}

func Import(req ImportRequest) (ImportResult, error) {
	if req.Mode != SkipDuplicates && req.Mode != UpdateDuplicates && req.Mode != CreateDuplicates {
		return ImportResult{}, ErrParse
	}
	p, err := PreviewWithOptions(req.Input, 100_000, req.Options)
	if err != nil {
		return ImportResult{}, err
	}
	out := ImportResult{Endpoints: make([]Endpoint, 0, p.Valid)}
	seen := map[string]bool{}
	for _, item := range p.Items {
		if item.Error != "" {
			continue
		}
		e := item.Result.Endpoint
		key := Identity(e)
		if seen[key] {
			if req.Mode == SkipDuplicates {
				out.Skipped++
				continue
			}
			if req.Mode == CreateDuplicates {
				e.ID = model.NewID()
				e.Name += " (duplicate)"
			}
		}
		seen[key] = true
		if existing, ok := req.Existing[key]; ok {
			switch req.Mode {
			case SkipDuplicates:
				out.Skipped++
				continue
			case UpdateDuplicates:
				e.ID = existing.ID
				out.Updated++
			}
		}
		if e.ID == "imported" {
			e.ID = model.NewID()
		}
		out.Endpoints = append(out.Endpoints, e)
		out.Created++
	}
	sort.Slice(out.Endpoints, func(i, j int) bool { return out.Endpoints[i].ID < out.Endpoints[j].ID })
	return out, nil
}

// CredentialFingerprint is for deduplication performed by a trusted persistence
// boundary. Never expose the credential itself or use this as a password hash.
func CredentialFingerprint(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	return hex.EncodeToString(sum[:])
}
