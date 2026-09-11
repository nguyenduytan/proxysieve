package buildinfo

import (
	"runtime"
	"testing"
)

func TestCurrent(t *testing.T) {
	t.Parallel()
	info := Current()
	if info.Name != "ProxySieve" || info.Author != "Tony Nguyen" || info.License != "Apache-2.0" {
		t.Fatalf("identity missing: %+v", info)
	}
	if info.Version == "" || info.Commit == "" || info.BuildDate == "" {
		t.Fatalf("missing build defaults: %+v", info)
	}
	if info.GoVersion != runtime.Version() || info.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("incorrect runtime metadata: %+v", info)
	}
	info.Author = "changed by caller"
	if Current().Author != Author {
		t.Fatal("snapshot mutation affected subsequent reads")
	}
}
