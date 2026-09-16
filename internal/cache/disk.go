package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	public "github.com/nguyenduytan/proxysieve/pkg/cache"
)

const diskRecordVersion = 1
const maxDiskRecordBytes = 12 << 20

type diskRecord struct {
	Version int        `json:"version"`
	Key     public.Key `json:"key"`
	Entry   Entry      `json:"entry"`
}

type diskMeta struct {
	path      string
	host      string
	expiresAt time.Time
	bytes     int64
}

type Disk struct {
	// ponytail: one lock keeps file/index accounting exact; shard only after measured contention.
	mu         sync.Mutex
	dir        string
	entries    map[string]diskMeta
	maxEntries int
	maxBytes   int64
	bytes      int64
	hits       uint64
	misses     uint64
	bypasses   uint64
	expired    uint64
	evictions  uint64
	served     uint64
}

func NewDisk(dir string, maxEntries int, maxBytes int64) (*Disk, error) {
	if dir == "" || strings.ContainsRune(dir, 0) || maxEntries < 1 || maxBytes < 1 {
		return nil, public.ErrInvalid
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, public.ErrInvalid
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	d := &Disk{dir: dir, entries: map[string]diskMeta{}, maxEntries: maxEntries, maxBytes: maxBytes}
	if err := d.load(); err != nil {
		return nil, err
	}
	return d, nil
}

func (*Disk) Kind() string { return "disk" }

func (d *Disk) Get(key public.Key, now time.Time) (Entry, bool) {
	if key.Validate() != nil {
		return Entry{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	id := diskID(key)
	meta, ok := d.entries[id]
	if !ok || !meta.expiresAt.After(now) {
		if ok {
			d.drop(id, meta)
			d.expired++
		}
		d.misses++
		return Entry{}, false
	}
	record, err := d.read(meta.path)
	if err != nil || diskID(record.Key) != id || record.Key.String() != key.String() || !record.Entry.ExpiresAt.Equal(meta.expiresAt) || !record.Entry.ExpiresAt.After(now) || int64(len(record.Entry.Body)) != meta.bytes {
		d.drop(id, meta)
		d.misses++
		return Entry{}, false
	}
	d.hits++
	d.served += uint64(len(record.Entry.Body))
	return clone(record.Entry), true
}

func (d *Disk) RecordBypass() {
	d.mu.Lock()
	d.bypasses++
	d.mu.Unlock()
}

func (d *Disk) Put(key public.Key, entry Entry) bool {
	host, ok := cacheHost(key)
	if !ok || entry.ExpiresAt.IsZero() || int64(len(entry.Body)) > d.maxBytes || len(entry.Body) > 8<<20 {
		return false
	}
	entry.host = ""
	data, err := json.Marshal(diskRecord{Version: diskRecordVersion, Key: key, Entry: clone(entry)})
	if err != nil || len(data) > maxDiskRecordBytes {
		return false
	}
	id := diskID(key)
	temporary, err := d.writeTemporary(id, data)
	if err != nil {
		return false
	}
	defer func() { _ = os.Remove(temporary) }()

	d.mu.Lock()
	defer d.mu.Unlock()
	old, exists := d.entries[id]
	oldBytes := old.bytes
	d.removeExpired(time.Now().UTC(), id)
	for (!exists && len(d.entries) >= d.maxEntries) || d.bytes-oldBytes+int64(len(entry.Body)) > d.maxBytes {
		if !d.evict(id) {
			return false
		}
	}
	target := filepath.Join(d.dir, id+".cache")
	if d.replace(temporary, target, exists) != nil {
		return false
	}
	d.entries[id] = diskMeta{path: target, host: host, expiresAt: entry.ExpiresAt, bytes: int64(len(entry.Body))}
	d.bytes += int64(len(entry.Body)) - oldBytes
	return true
}

func (d *Disk) Stats(now time.Time) Stats {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.removeExpired(now, "")
	ratio := float64(0)
	if requests := d.hits + d.misses; requests > 0 {
		ratio = float64(d.hits) / float64(requests)
	}
	return Stats{Entries: len(d.entries), Bytes: d.bytes, MaxEntries: d.maxEntries, MaxBytes: d.maxBytes, Hits: d.hits, Misses: d.misses, Bypasses: d.bypasses, Expired: d.expired, Evictions: d.evictions, BytesServed: d.served, HitRatio: ratio}
}

func (d *Disk) Purge() PurgeResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := PurgeResult{}
	for id, meta := range d.entries {
		if err := os.Remove(meta.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
		result.Entries++
		result.Bytes += meta.bytes
		delete(d.entries, id)
	}
	d.bytes -= result.Bytes
	return result
}

func (d *Disk) PurgeDomain(domain string) PurgeResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	result := PurgeResult{}
	for id, meta := range d.entries {
		if meta.host != domain {
			continue
		}
		if err := os.Remove(meta.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
		result.Entries++
		result.Bytes += meta.bytes
		delete(d.entries, id)
	}
	d.bytes -= result.Bytes
	return result
}

func (d *Disk) load() error {
	directory, err := os.Open(d.dir)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	for {
		files, readErr := directory.ReadDir(256)
		for _, file := range files {
			name := file.Name()
			if recovered, handled, err := d.recoverTemporary(name); err != nil {
				return err
			} else if handled {
				if recovered == "" {
					continue
				}
				name = recovered
			}
			id := strings.TrimSuffix(name, ".cache")
			if !file.Type().IsRegular() || !strings.HasSuffix(name, ".cache") || !validDiskID(id) {
				continue
			}
			path := filepath.Join(d.dir, name)
			if err := os.Chmod(path, 0600); err != nil {
				_ = os.Remove(path)
				continue
			}
			record, err := d.read(path)
			host, valid := cacheHost(record.Key)
			if err != nil || !valid || diskID(record.Key) != id || !record.Entry.ExpiresAt.After(time.Now().UTC()) || int64(len(record.Entry.Body)) > d.maxBytes {
				_ = os.Remove(path)
				continue
			}
			d.entries[id] = diskMeta{path: path, host: host, expiresAt: record.Entry.ExpiresAt, bytes: int64(len(record.Entry.Body))}
			d.bytes += int64(len(record.Entry.Body))
			for len(d.entries) > d.maxEntries || d.bytes > d.maxBytes {
				if !d.evict("") {
					return public.ErrInvalid
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func (d *Disk) read(path string) (diskRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return diskRecord{}, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxDiskRecordBytes+1))
	if err != nil || len(data) > maxDiskRecordBytes {
		return diskRecord{}, public.ErrInvalid
	}
	var record diskRecord
	if json.Unmarshal(data, &record) != nil || record.Version != diskRecordVersion || record.Key.Validate() != nil || record.Entry.ExpiresAt.IsZero() || len(record.Entry.Body) > 8<<20 {
		return diskRecord{}, public.ErrInvalid
	}
	return record, nil
}

func (d *Disk) writeTemporary(id string, data []byte) (string, error) {
	file, err := os.CreateTemp(d.dir, id+"-*.tmp")
	if err != nil {
		return "", err
	}
	path := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if file.Chmod(0600) != nil {
		return "", public.ErrInvalid
	}
	if _, err = file.Write(data); err != nil || file.Sync() != nil || file.Close() != nil {
		return "", public.ErrInvalid
	}
	ok = true
	return path, nil
}

func (d *Disk) replace(temporary, target string, exists bool) error {
	if !exists {
		return os.Rename(temporary, target)
	}
	backup := target + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func (d *Disk) removeExpired(now time.Time, except string) {
	for id, meta := range d.entries {
		if id != except && !meta.expiresAt.After(now) {
			d.drop(id, meta)
			d.expired++
		}
	}
}

func (d *Disk) evict(except string) bool {
	var candidate string
	var selected diskMeta
	for id, meta := range d.entries {
		if id == except || candidate != "" && (meta.expiresAt.After(selected.expiresAt) || meta.expiresAt.Equal(selected.expiresAt) && id > candidate) {
			continue
		}
		candidate, selected = id, meta
	}
	if candidate == "" {
		return false
	}
	if err := os.Remove(selected.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false
	}
	delete(d.entries, candidate)
	d.bytes -= selected.bytes
	d.evictions++
	return true
}

func (d *Disk) drop(id string, meta diskMeta) {
	_ = os.Remove(meta.path)
	delete(d.entries, id)
	d.bytes -= meta.bytes
}

func diskID(key public.Key) string {
	sum := sha256.Sum256([]byte(key.String()))
	return hex.EncodeToString(sum[:])
}

func validDiskID(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (d *Disk) recoverTemporary(name string) (string, bool, error) {
	path := filepath.Join(d.dir, name)
	if strings.HasSuffix(name, ".cache.old") {
		id := strings.TrimSuffix(name, ".cache.old")
		if !validDiskID(id) {
			return "", false, nil
		}
		target := filepath.Join(d.dir, id+".cache")
		if _, err := os.Lstat(target); err == nil {
			return "", true, os.Remove(path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", true, err
		}
		if err := os.Rename(path, target); err != nil {
			return "", true, err
		}
		return id + ".cache", true, nil
	}
	if len(name) > sha256.Size*2+len("-.tmp") && name[sha256.Size*2] == '-' && strings.HasSuffix(name, ".tmp") && validDiskID(name[:sha256.Size*2]) {
		return "", true, os.Remove(path)
	}
	return "", false, nil
}

func cacheHost(key public.Key) (string, bool) {
	if key.Validate() != nil {
		return "", false
	}
	parsed, err := url.Parse(key.URL)
	if err != nil || parsed.Hostname() == "" {
		return "", false
	}
	return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")), true
}
