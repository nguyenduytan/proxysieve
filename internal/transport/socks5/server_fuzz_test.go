package socks5

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/nguyenduytan/proxysieve/pkg/proxy"
)

func FuzzSOCKSRequest(f *testing.F) {
	f.Add([]byte{5, 1, 0, 1, 127, 0, 0, 1, 0x01, 0xbb})
	f.Add([]byte{5, 1, 0, 3, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0, 80})
	f.Add([]byte{5, 1, 0, 3, 7, '0', '0', '0', '0', '0', '0', ' ', 0, 80})
	f.Add([]byte{5, 3, 0, 1})
	f.Fuzz(func(t *testing.T, data []byte) {
		host, port, ok := readRequest(bufio.NewReader(bytes.NewReader(data)))
		if ok && (!proxy.ValidHost(host) || port == 0) {
			t.Fatal("invalid SOCKS target accepted")
		}
	})
}
