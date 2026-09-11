package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/nguyenduytan/proxysieve/pkg/model"
)

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
	if maxLines < 1 || maxLines > 100_000 || len(input) > 8<<20 {
		return ImportPreview{}, ErrParse
	}
	out := ImportPreview{Protocols: map[Protocol]int{}}
	seen := map[string]bool{}
	for line, raw := range strings.Split(input, "\n") {
		if line >= maxLines {
			return ImportPreview{}, ErrParse
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		item := PreviewItem{Line: line + 1}
		r, err := Parse(raw)
		if err != nil {
			item.Error = ErrParse.Error()
			out.Invalid++
		} else {
			item.Result = r
			out.Valid++
			out.Protocols[r.Endpoint.Protocol]++
			key := Identity(r.Endpoint)
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

// Identity excludes credentials because Parse deliberately does not retain them.
// A later authenticated import must pass a SecretRef explicitly to persistence.
func Identity(e Endpoint) string {
	return string(e.Protocol) + "|" + strings.ToLower(e.Host) + "|" + strconv.Itoa(int(e.Port))
}

type ImportRequest struct {
	Input    string
	Mode     DuplicateMode
	Existing map[string]Endpoint
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
	p, err := Preview(req.Input, 100_000)
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
