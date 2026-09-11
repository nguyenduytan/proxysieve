package security

import (
	"bytes"
	"crypto/rand"
	"github.com/nguyenduytan/proxysieve/pkg/secret"
	"net/http"
	"strings"
	"testing"
)

func TestCipher(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	c, err := NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := secret.New([]byte("fake-sensitive-payload"))
	ref := secret.Ref("secret://test/credential")
	a, err := c.Seal(ref, v)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Seal(ref, v)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) || bytes.Contains(a, v.Reveal()) {
		t.Fatal("nonce reused or plaintext stored")
	}
	got, err := c.Open(ref, a)
	if err != nil || !bytes.Equal(got.Reveal(), v.Reveal()) {
		t.Fatal(err)
	}
	if _, err = c.Open("secret://test/other", a); err == nil {
		t.Fatal("record substitution succeeded")
	}
	for i := range a {
		bad := bytes.Clone(a)
		bad[i] ^= 1
		if _, err = c.Open(ref, bad); err == nil {
			t.Fatalf("tamper accepted at %d", i)
		}
	}
	other, _ := NewCipher(make([]byte, 32))
	if _, err = other.Open(ref, a); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err = NewCipher(make([]byte, 16)); err == nil {
		t.Fatal("non-256 key accepted")
	}
}
func TestRedaction(t *testing.T) {
	h := http.Header{"authorization": {"fake-auth"}, "Proxy-Authorization": {"fake-proxy"}, "X_API_KEY": {"fake-api"}, "Cookie": {"fake-cookie"}, "Accept": {"application/json"}}
	r := RedactHeaders(h)
	for k, v := range r {
		if k != "Accept" && v[0] != secret.Redacted {
			t.Fatal("header leaked", k)
		}
	}
	r["Accept"][0] = "changed"
	if h["Accept"][0] != "application/json" {
		t.Fatal("input changed")
	}
	got := RedactURL("https://user:fake-password@example.invalid/path?unknown=fake-token#fake-fragment")
	for _, s := range []string{"user", "fake-password", "fake-token", "fake-fragment"} {
		if strings.Contains(got, s) {
			t.Fatal("url leaked", got)
		}
	}
	if RedactURL(":bad") != secret.Redacted {
		t.Fatal("invalid URL echoed")
	}
}
