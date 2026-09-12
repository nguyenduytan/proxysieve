package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/nguyenduytan/proxysieve/pkg/auth"
	"github.com/nguyenduytan/proxysieve/pkg/model"
	"github.com/nguyenduytan/proxysieve/pkg/store"
	"time"
)

type ClientRecord struct {
	Client   auth.Client `json:"client"`
	Revision int64       `json:"revision"`
}

func (s *Store) ListClients(ctx context.Context, limit int) ([]auth.Client, error) {
	if limit < 1 || limit > 1000 {
		return nil, store.ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, "SELECT document FROM clients ORDER BY id LIMIT ?", limit)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer func() { _ = rows.Close() }()
	clients := make([]auth.Client, 0, limit)
	for rows.Next() {
		var body []byte
		var client auth.Client
		if err = rows.Scan(&body); err != nil || json.Unmarshal(body, &client) != nil || !validClient(client) {
			return nil, store.ErrSchema
		}
		clients = append(clients, client)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError(ctx, err)
	}
	return clients, nil
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

func (s *Store) CreateClient(ctx context.Context, client auth.Client) error {
	if !validClient(client) {
		return store.ErrInvalid
	}
	body, err := json.Marshal(client)
	if err != nil {
		return store.ErrInvalid
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO clients(id,revision,document) VALUES (?,1,?)", string(client.ID), string(body))
	if err != nil {
		return safeError(ctx, err)
	}
	return nil
}
func (s *Store) FindClient(ctx context.Context, id model.ID) (ClientRecord, error) {
	if !id.Valid() {
		return ClientRecord{}, store.ErrInvalid
	}
	var body []byte
	var record ClientRecord
	err := s.db.QueryRowContext(ctx, "SELECT document,revision FROM clients WHERE id=?", string(id)).Scan(&body, &record.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return ClientRecord{}, store.ErrNotFound
	}
	if err != nil {
		return ClientRecord{}, safeError(ctx, err)
	}
	if json.Unmarshal(body, &record.Client) != nil || !validClient(record.Client) || record.Client.ID != id {
		return ClientRecord{}, store.ErrSchema
	}
	return record, nil
}
func (s *Store) FindClientAuth(ctx context.Context, id model.ID) (auth.Client, error) {
	record, err := s.FindClient(ctx, id)
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
func validClient(client auth.Client) bool {
	return client.ID.Valid() && len(client.Name) >= 1 && len(client.Name) <= 256 && client.AuthMethod != "" && !client.CreatedAt.IsZero()
}
func validAPIKey(key auth.APIKey) bool {
	return key.ID.Valid() && key.ClientID.Valid() && len(key.Prefix) >= 8 && len(key.Prefix) <= 32 && !key.CreatedAt.IsZero()
}
