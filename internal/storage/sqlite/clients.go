package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
)

func (s *Store) ListClients(ctx context.Context, page store.Page) ([]store.ClientRecord, error) {
	if err := page.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,document,revision FROM clients WHERE id>? ORDER BY id LIMIT ?", string(page.After), page.Limit)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	records := make([]store.ClientRecord, 0, page.Limit)
	for rows.Next() {
		var id string
		var body []byte
		var record store.ClientRecord
		if err = rows.Scan(&id, &body, &record.Revision); err != nil || json.Unmarshal(body, &record.Client) != nil || !record.Client.Validate() || string(record.Client.ID) != id {
			return nil, store.ErrSchema
		}
		records = append(records, store.ClientRecord{Client: record.Client.Clone(), Revision: record.Revision})
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return records, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, clientID model.ID, limit int) ([]auth.APIKey, error) {
	if !clientID.Valid() || limit < 1 || limit > 1000 {
		return nil, store.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,client_id,prefix,created_at,revoked_at FROM api_keys WHERE client_id=? ORDER BY created_at DESC,id LIMIT ?", string(clientID), limit)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	keys := make([]auth.APIKey, 0, limit)
	for rows.Next() {
		var key auth.APIKey
		var created int64
		var revoked sql.NullInt64
		if err = rows.Scan(&key.ID, &key.ClientID, &key.Prefix, &created, &revoked); err != nil {
			return nil, safeError(ctx, err)
		}
		key.CreatedAt = time.Unix(0, created).UTC()
		if revoked.Valid {
			at := time.Unix(0, revoked.Int64).UTC()
			key.RevokedAt = &at
		}
		if !validAPIKey(key) {
			return nil, store.ErrSchema
		}
		keys = append(keys, key)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return keys, nil
}

func (s *Store) PutClient(ctx context.Context, client auth.Client, expected int64) (store.ClientRecord, error) {
	if !client.Validate() || expected < 0 || expected == math.MaxInt64 {
		return store.ClientRecord{}, store.ErrInvalid
	}
	body, err := json.Marshal(client)
	if err != nil || len(body) > 262144 {
		return store.ClientRecord{}, store.ErrInvalid
	}
	var result sql.Result
	if expected == 0 {
		result, err = s.db.ExecContext(ctx, "INSERT INTO clients(id,revision,document) VALUES (?,1,?) ON CONFLICT(id) DO NOTHING", string(client.ID), string(body))
	} else {
		result, err = s.db.ExecContext(ctx, "UPDATE clients SET document=?,revision=revision+1 WHERE id=? AND revision=?", string(body), string(client.ID), expected)
	}
	if err != nil {
		return store.ClientRecord{}, safeError(ctx, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return store.ClientRecord{}, safeError(ctx, err)
	}
	if count == 0 {
		if expected > 0 {
			if _, err = s.GetClient(ctx, client.ID); err != nil {
				return store.ClientRecord{}, err
			}
		}
		return store.ClientRecord{}, store.ErrConflict
	}
	return store.ClientRecord{Client: client.Clone(), Revision: expected + 1}, nil
}

func (s *Store) GetClient(ctx context.Context, id model.ID) (store.ClientRecord, error) {
	if !id.Valid() {
		return store.ClientRecord{}, store.ErrInvalid
	}
	var body []byte
	var record store.ClientRecord
	err := s.db.QueryRowContext(ctx, "SELECT document,revision FROM clients WHERE id=?", string(id)).Scan(&body, &record.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ClientRecord{}, store.ErrNotFound
	}
	if err != nil {
		return store.ClientRecord{}, safeError(ctx, err)
	}
	if json.Unmarshal(body, &record.Client) != nil || !record.Client.Validate() || record.Client.ID != id {
		return store.ClientRecord{}, store.ErrSchema
	}
	record.Client = record.Client.Clone()
	return record, nil
}

func (s *Store) DeleteClient(ctx context.Context, id model.ID, expected int64) error {
	if !id.Valid() || expected < 1 {
		return store.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM clients WHERE id=? AND revision=?", string(id), expected)
	if err != nil {
		return safeError(ctx, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if count == 0 {
		if _, err = s.GetClient(ctx, id); err != nil {
			return err
		}
		return store.ErrConflict
	}
	return nil
}

func (s *Store) FindClientAuth(ctx context.Context, id model.ID) (auth.Client, error) {
	record, err := s.GetClient(ctx, id)
	return record.Client, err
}
func (s *Store) CreateAPIKey(ctx context.Context, key auth.APIKey, hash [32]byte) (auth.APIKey, error) {
	if !validAPIKey(key) {
		return auth.APIKey{}, store.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, "INSERT INTO api_keys(id,client_id,prefix,token_hash,created_at,revoked_at) VALUES (?,?,?,?,?,NULL)", string(key.ID), string(key.ClientID), key.Prefix, hash[:], key.CreatedAt.UTC().UnixNano())
	if err != nil {
		return auth.APIKey{}, safeError(ctx, err)
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return auth.APIKey{}, safeError(ctx, err)
	}
	return key, nil
}
func (s *Store) FindAPIKey(ctx context.Context, hash [32]byte) (auth.APIKey, error) {
	var key auth.APIKey
	var created int64
	var revoked sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT id,client_id,prefix,created_at,revoked_at FROM api_keys WHERE token_hash=?", hash[:]).Scan(&key.ID, &key.ClientID, &key.Prefix, &created, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.APIKey{}, store.ErrNotFound
	}
	if err != nil {
		return auth.APIKey{}, safeError(ctx, err)
	}
	key.CreatedAt = time.Unix(0, created).UTC()
	if revoked.Valid {
		value := time.Unix(0, revoked.Int64).UTC()
		key.RevokedAt = &value
	}
	if !validAPIKey(key) {
		return auth.APIKey{}, store.ErrSchema
	}
	return key, nil
}
func (s *Store) RevokeAPIKey(ctx context.Context, clientID, id model.ID, at time.Time) error {
	if !clientID.Valid() || !id.Valid() {
		return store.ErrInvalid
	}
	result, err := s.db.ExecContext(ctx, "UPDATE api_keys SET revoked_at=? WHERE id=? AND client_id=? AND revoked_at IS NULL", at.UTC().UnixNano(), string(id), string(clientID))
	if err != nil {
		return safeError(ctx, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return safeError(ctx, err)
	}
	if count != 1 {
		return store.ErrNotFound
	}
	return nil
}
func validAPIKey(key auth.APIKey) bool {
	return key.ID.Valid() && key.ClientID.Valid() && len(key.Prefix) >= 8 && len(key.Prefix) <= 32 && !key.CreatedAt.IsZero()
}
