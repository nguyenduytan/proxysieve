package proxy

import (
	"strings"
	"testing"
)

func TestPreview(t *testing.T) {
	p, err := Preview("http://a.example.invalid:8080\n\nhttp://a.example.invalid:8080\nsocks5://b.example.invalid:1080\nbad", 10)
	if err != nil || p.Valid != 3 || p.Invalid != 1 || p.Duplicates != 1 || p.Protocols[HTTP] != 2 || p.Protocols[SOCKS5] != 1 {
		t.Fatalf("%+v %v", p, err)
	}
}
func TestImportModes(t *testing.T) {
	input := "http://a.example.invalid:8080\nhttp://a.example.invalid:8080"
	existing := map[string]Endpoint{Identity(Endpoint{Protocol: HTTP, Host: "a.example.invalid", Port: 8080}): {ID: "existing", Protocol: HTTP, Host: "a.example.invalid", Port: 8080}}
	for _, mode := range []struct {
		m           DuplicateMode
		skip, count int
	}{{SkipDuplicates, 2, 0}, {UpdateDuplicates, 0, 2}, {CreateDuplicates, 0, 2}} {
		r, err := Import(ImportRequest{Input: input, Mode: mode.m, Existing: existing})
		if err != nil || r.Skipped != mode.skip || len(r.Endpoints) != mode.count {
			t.Fatalf("%s %+v %v", mode.m, r, err)
		}
		for _, e := range r.Endpoints {
			if e.ID == "imported" || strings.Contains(e.String(), "fake-password") {
				t.Fatal("unsafe result", e)
			}
		}
	}
}
func TestImportBounds(t *testing.T) {
	if _, err := Preview(strings.Repeat("http://a.example.invalid:80\n", 100_001), 100_000); err == nil {
		t.Fatal("line bound ignored")
	}
	if _, err := Preview(strings.Repeat("x", (8<<20)+1), 1); err == nil {
		t.Fatal("byte bound ignored")
	}
	if _, err := Import(ImportRequest{Input: "x", Mode: "invalid"}); err == nil {
		t.Fatal("mode accepted")
	}
	if CredentialFingerprint("fake-password") == "" {
		t.Fatal("empty fingerprint")
	}
}

func TestStructuredImportMapping(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		options ImportOptions
	}{
		{
			name:  "csv",
			input: "kind,address,listen\nsocks5,proxy.example.invalid,1080\nhttp,second.example.invalid,8080",
			options: ImportOptions{Format: ImportCSV, Mapping: ImportMapping{
				ProtocolField: "kind", HostField: "address", PortField: "listen",
			}},
		},
		{
			name:  "json object",
			input: `{"proxies":[{"url":"http://user:secret@proxy.example.invalid:8080"},{"url":"socks5://second.example.invalid:1080"}]}`,
			options: ImportOptions{Format: ImportJSON, Mapping: ImportMapping{
				ItemsField: "proxies", EndpointField: "url",
			}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preview, err := PreviewWithOptions(test.input, 10, test.options)
			if err != nil || preview.Valid != 2 || preview.Invalid != 0 {
				t.Fatal(preview, err)
			}
			result, err := Import(ImportRequest{Input: test.input, Mode: SkipDuplicates, Options: test.options})
			if err != nil || len(result.Endpoints) != 2 {
				t.Fatal(result, err)
			}
			for _, endpoint := range result.Endpoints {
				if endpoint.CredentialRef != "" || strings.Contains(endpoint.String(), "secret") {
					t.Fatal("credential escaped import boundary", endpoint)
				}
			}
		})
	}
}

func TestStructuredImportRejectsMalformedAndBounds(t *testing.T) {
	invalid := []struct {
		input   string
		options ImportOptions
		max     int
	}{
		{"a,a\n1,2", ImportOptions{Format: ImportCSV}, 10},
		{"host,port\nexample.invalid,80\nsecond.invalid,81", ImportOptions{Format: ImportCSV}, 1},
		{`{"items":{}}`, ImportOptions{Format: ImportJSON}, 10},
		{`[{"host":"example.invalid","port":80}] trailing`, ImportOptions{Format: ImportJSON}, 10},
		{"anything", ImportOptions{Format: "xml"}, 10},
	}
	for i, test := range invalid {
		if _, err := PreviewWithOptions(test.input, test.max, test.options); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
