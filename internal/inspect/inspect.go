package inspect

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nguyenduytan/proxysieve/internal/security"
	"github.com/nguyenduytan/proxysieve/pkg/proxy"
)

const (
	maxBundleBytes  = 64 << 10
	maxCertificates = 1024
	maxObservations = 1000
)

var (
	ErrInvalid     = errors.New("invalid inspect configuration or CA")
	ErrExists      = errors.New("inspect CA already exists")
	ErrNotFound    = errors.New("inspect CA not found")
	ErrUnavailable = errors.New("inspect CA unavailable")
)

type Info struct {
	Fingerprint string
	NotAfter    time.Time
	Certificate []byte
}

type Manager struct {
	certificate    *x509.Certificate
	certificatePEM []byte
	signer         crypto.Signer
	include        []string
	exclude        []string
	captureHeaders bool

	mu     sync.Mutex
	cache  map[string]cachedCertificate
	recent []Observation
	now    func() time.Time
}

type Observation struct {
	At              time.Time   `json:"at"`
	Method          string      `json:"method"`
	URL             string      `json:"url"`
	StatusCode      int         `json:"status_code"`
	ContentType     string      `json:"content_type,omitempty"`
	RequestBytes    uint64      `json:"request_bytes"`
	ResponseBytes   uint64      `json:"response_bytes"`
	RequestHeaders  http.Header `json:"request_headers,omitempty"`
	ResponseHeaders http.Header `json:"response_headers,omitempty"`
}

type cachedCertificate struct {
	certificate tls.Certificate
	expires     time.Time
}

func Create(dataDir string, rotate bool) (Info, error) {
	directory, target, err := paths(dataDir)
	if err != nil {
		return Info{}, err
	}
	if err = os.MkdirAll(directory, 0700); err != nil || os.Chmod(directory, 0700) != nil {
		return Info{}, ErrUnavailable
	}
	if rotate {
		if _, err = ReadInfo(dataDir); err != nil {
			return Info{}, err
		}
	}
	bundle, info, err := generate(time.Now().UTC())
	if err != nil {
		return Info{}, err
	}
	temporary, err := writeTemporary(directory, bundle)
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = os.Remove(temporary) }()
	if !rotate {
		if err = installFile(temporary, target); err != nil {
			if errors.Is(err, os.ErrExist) {
				return Info{}, ErrExists
			}
			return Info{}, ErrUnavailable
		}
		return info, verifySecureFile(target)
	}
	if err = replaceFile(temporary, target); err != nil {
		return Info{}, ErrUnavailable
	}
	return info, verifySecureFile(target)
}

func Open(dataDir string, include, exclude []string, captureHeaders bool) (*Manager, error) {
	_, target, err := paths(dataDir)
	if err != nil {
		return nil, err
	}
	certificate, signer, certificatePEM, err := load(target)
	if err != nil {
		return nil, err
	}
	return &Manager{certificate: certificate, certificatePEM: certificatePEM, signer: signer, include: normalize(include), exclude: normalize(exclude), captureHeaders: captureHeaders, cache: make(map[string]cachedCertificate), now: func() time.Time { return time.Now().UTC() }}, nil
}

func ReadInfo(dataDir string) (Info, error) {
	_, target, err := paths(dataDir)
	if err != nil {
		return Info{}, err
	}
	certificate, _, certificatePEM, err := load(target)
	if err != nil {
		return Info{}, err
	}
	return info(certificate, certificatePEM), nil
}

func Export(dataDir, destination string) error {
	value, err := ReadInfo(dataDir)
	if err != nil {
		return err
	}
	if destination == "" || strings.ContainsRune(destination, 0) {
		return ErrInvalid
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if errors.Is(err, os.ErrExist) {
		return ErrExists
	}
	if err != nil {
		return ErrUnavailable
	}
	if _, err = file.Write(value.Certificate); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(destination)
		return ErrUnavailable
	}
	return nil
}

func (m *Manager) TLSConfig(host string) (*tls.Config, bool, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if !m.matches(host) {
		return nil, false, nil
	}
	certificate, err := m.hostCertificate(host)
	if err != nil {
		return nil, true, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}, Certificates: []tls.Certificate{certificate}}, true, nil
}

func (m *Manager) CertificatePEM() []byte {
	return append([]byte(nil), m.certificatePEM...)
}

func (m *Manager) Record(method, rawURL string, status int, requestHeaders, responseHeaders http.Header, requestBytes, responseBytes uint64) {
	observation := Observation{At: m.now(), Method: method, URL: security.RedactURL(rawURL), StatusCode: status, ContentType: responseHeaders.Get("Content-Type"), RequestBytes: requestBytes, ResponseBytes: responseBytes}
	if m.captureHeaders {
		observation.RequestHeaders = boundedHeaders(security.RedactHeaders(requestHeaders))
		observation.ResponseHeaders = boundedHeaders(security.RedactHeaders(responseHeaders))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.recent) == maxObservations {
		copy(m.recent, m.recent[1:])
		m.recent = m.recent[:maxObservations-1]
	}
	m.recent = append(m.recent, observation)
}

func (m *Manager) Recent(limit int) []Observation {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	limit = min(limit, len(m.recent))
	result := make([]Observation, limit)
	for index := range limit {
		source := m.recent[len(m.recent)-1-index]
		source.RequestHeaders = source.RequestHeaders.Clone()
		source.ResponseHeaders = source.ResponseHeaders.Clone()
		result[index] = source
	}
	return result
}

func (m *Manager) matches(host string) bool {
	for _, pattern := range m.exclude {
		if match, _ := path.Match(pattern, host); match {
			return false
		}
	}
	for _, pattern := range m.include {
		if match, _ := path.Match(pattern, host); match {
			return true
		}
	}
	return false
}

func (m *Manager) hostCertificate(host string) (tls.Certificate, error) {
	if !proxy.ValidHost(host) {
		return tls.Certificate{}, ErrInvalid
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if cached, ok := m.cache[host]; ok && now.Before(cached.expires) {
		return cached.certificate, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, ErrUnavailable
	}
	serial, err := randomSerial()
	if err != nil {
		return tls.Certificate{}, ErrUnavailable
	}
	notAfter := now.Add(24 * time.Hour)
	if notAfter.After(m.certificate.NotAfter) {
		notAfter = m.certificate.NotAfter
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, NotBefore: now.Add(-5 * time.Minute), NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, m.certificate, &key.PublicKey, m.signer)
	if err != nil {
		return tls.Certificate{}, ErrUnavailable
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, ErrUnavailable
	}
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		return tls.Certificate{}, ErrUnavailable
	}
	if len(m.cache) >= maxCertificates {
		// ponytail: whole-cache eviction keeps the trust boundary bounded; add LRU only if host churn is measured.
		clear(m.cache)
	}
	m.cache[host] = cachedCertificate{certificate: certificate, expires: notAfter.Add(-time.Hour)}
	return certificate, nil
}

func generate(now time.Time) ([]byte, Info, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, Info{}, ErrUnavailable
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, Info{}, ErrUnavailable
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{Organization: []string{"ProxySieve"}, CommonName: "ProxySieve Local Inspect CA"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, Info{}, ErrUnavailable
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, Info{}, ErrUnavailable
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, Info{}, ErrUnavailable
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	bundle := append(append([]byte(nil), certificatePEM...), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})...)
	return bundle, info(certificate, certificatePEM), nil
}

func load(target string) (*x509.Certificate, crypto.Signer, []byte, error) {
	if err := verifySecureFile(target); err != nil {
		return nil, nil, nil, err
	}
	file, err := os.Open(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, nil, ErrUnavailable
	}
	defer func() { _ = file.Close() }()
	bundle, err := io.ReadAll(io.LimitReader(file, maxBundleBytes+1))
	if err != nil || len(bundle) > maxBundleBytes {
		return nil, nil, nil, ErrInvalid
	}
	certificateBlock, rest := pem.Decode(bundle)
	keyBlock, trailing := pem.Decode(rest)
	if certificateBlock == nil || certificateBlock.Type != "CERTIFICATE" || keyBlock == nil || keyBlock.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(trailing))) != 0 {
		return nil, nil, nil, ErrInvalid
	}
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	now := time.Now().UTC()
	if err != nil || !certificate.IsCA || !certificate.BasicConstraintsValid || certificate.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) {
		return nil, nil, nil, ErrInvalid
	}
	key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	signer, ok := key.(crypto.Signer)
	if err != nil || !ok {
		return nil, nil, nil, ErrInvalid
	}
	public, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return nil, nil, nil, ErrInvalid
	}
	want, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil || !bytes.Equal(public, want) {
		return nil, nil, nil, ErrInvalid
	}
	certificatePEM := pem.EncodeToMemory(certificateBlock)
	return certificate, signer, certificatePEM, nil
}

func paths(dataDir string) (string, string, error) {
	if dataDir == "" || strings.ContainsRune(dataDir, 0) {
		return "", "", ErrInvalid
	}
	directory := filepath.Join(dataDir, "inspect")
	return directory, filepath.Join(directory, "ca.pem"), nil
}

func writeTemporary(directory string, bundle []byte) (string, error) {
	file, err := os.CreateTemp(directory, ".ca-*.tmp")
	if err != nil {
		return "", ErrUnavailable
	}
	name := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if file.Chmod(0600) != nil || secureFile(name) != nil {
		return "", ErrUnavailable
	}
	if _, err = file.Write(bundle); err != nil || file.Sync() != nil || file.Close() != nil {
		return "", ErrUnavailable
	}
	ok = true
	return name, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, limit)
}

func info(certificate *x509.Certificate, certificatePEM []byte) Info {
	digest := sha256.Sum256(certificate.Raw)
	raw := strings.ToUpper(hex.EncodeToString(digest[:]))
	parts := make([]string, 0, len(raw)/2)
	for index := 0; index < len(raw); index += 2 {
		parts = append(parts, raw[index:index+2])
	}
	return Info{Fingerprint: strings.Join(parts, ":"), NotAfter: certificate.NotAfter.UTC(), Certificate: append([]byte(nil), certificatePEM...)}
}

func normalize(patterns []string) []string {
	result := make([]string, len(patterns))
	for index, pattern := range patterns {
		result[index] = strings.ToLower(strings.TrimSuffix(pattern, "."))
	}
	return result
}

func boundedHeaders(headers http.Header) http.Header {
	result := make(http.Header)
	count := 0
	for name, values := range headers {
		if count == 64 {
			break
		}
		for _, value := range values[:min(len(values), 16)] {
			if len(value) > 4096 {
				value = value[:4096]
			}
			result.Add(name, value)
		}
		count++
	}
	return result
}
