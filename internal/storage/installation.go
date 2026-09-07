package storage

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Installation holds the per-GitHub-App-installation configuration stored
// in the database. Each row tracks API key settings and free-tier usage.
type Installation struct {
	ID               int64
	InstallationID   int64
	AccountLogin     string
	Provider         string // groq | openai | claude | gemini | grok — empty = use server default
	APIKeyEncrypted  string // AES-256-GCM encrypted, base64-encoded
	Model            string // optional custom model override
	FreeReviewsUsed  int
	FreeReviewsLimit int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// HasCustomKey returns true when the installation has provided its own API key.
func (inst *Installation) HasCustomKey() bool {
	return inst.Provider != "" && inst.APIKeyEncrypted != ""
}

// IsOverFreeLimit returns true when free usage is exhausted and no custom key is set.
func (inst *Installation) IsOverFreeLimit() bool {
	return !inst.HasCustomKey() && inst.FreeReviewsUsed >= inst.FreeReviewsLimit
}

// Store provides CRUD operations for Installation records and handles
// encryption/decryption of API keys at rest.
type Store struct {
	db  *sql.DB
	gcm cipher.AEAD
}

// NewStore creates a Store.
//
// encryptionKeyB64 may be empty: review memory (pr_reviews / posted_comments)
// still works. Encrypting BYOK API keys requires a base64-encoded 32-byte key
// (generate with: openssl rand -base64 32).
func NewStore(db *sql.DB, encryptionKeyB64 string) (*Store, error) {
	s := &Store{db: db}
	if strings.TrimSpace(encryptionKeyB64) == "" {
		return s, nil
	}

	raw, err := base64.StdEncoding.DecodeString(encryptionKeyB64)
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf(
			"ENCRYPTION_KEY must be a base64-encoded 32-byte key (44 chars). Generate with: openssl rand -base64 32",
		)
	}

	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("creating AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	s.gcm = gcm
	return s, nil
}

// HasEncryption reports whether API keys can be stored encrypted (BYOK /setup).
func (s *Store) HasEncryption() bool {
	return s != nil && s.gcm != nil
}

// GetOrCreate returns the Installation for the given GitHub installation ID,
// creating a new record with defaults if it doesn't exist yet.
func (s *Store) GetOrCreate(ctx context.Context, installationID int64, accountLogin string) (*Installation, error) {
	var inst Installation
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO installations (installation_id, account_login, free_reviews_limit)
		VALUES ($1, $2, $3)
		ON CONFLICT (installation_id) DO UPDATE
			SET account_login = EXCLUDED.account_login,
			    updated_at    = NOW()
		RETURNING id, installation_id, account_login, provider, api_key_encrypted, model,
		          free_reviews_used, free_reviews_limit, created_at, updated_at
	`, installationID, accountLogin, defaultFreeLimit).Scan(
		&inst.ID, &inst.InstallationID, &inst.AccountLogin,
		&inst.Provider, &inst.APIKeyEncrypted, &inst.Model,
		&inst.FreeReviewsUsed, &inst.FreeReviewsLimit,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get-or-create installation %d: %w", installationID, err)
	}
	return &inst, nil
}

// Get fetches the Installation by GitHub installation ID.
// Returns sql.ErrNoRows if not found.
func (s *Store) Get(ctx context.Context, installationID int64) (*Installation, error) {
	var inst Installation
	err := s.db.QueryRowContext(ctx, `
		SELECT id, installation_id, account_login, provider, api_key_encrypted, model,
		       free_reviews_used, free_reviews_limit, created_at, updated_at
		FROM installations WHERE installation_id = $1
	`, installationID).Scan(
		&inst.ID, &inst.InstallationID, &inst.AccountLogin,
		&inst.Provider, &inst.APIKeyEncrypted, &inst.Model,
		&inst.FreeReviewsUsed, &inst.FreeReviewsLimit,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

// IncrementUsage atomically increments the free review counter.
func (s *Store) IncrementUsage(ctx context.Context, installationID int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE installations
		SET free_reviews_used = free_reviews_used + 1, updated_at = NOW()
		WHERE installation_id = $1
	`, installationID)
	return err
}

// UpdateConfig saves the installation's chosen provider and encrypted API key.
// Pass empty strings to clear a previously saved key (revert to free tier).
func (s *Store) UpdateConfig(ctx context.Context, installationID int64, provider, apiKey, model string) error {
	encrypted := ""
	if apiKey != "" {
		var err error
		encrypted, err = s.encrypt(apiKey)
		if err != nil {
			return fmt.Errorf("encrypting API key: %w", err)
		}
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE installations
		SET provider = $1, api_key_encrypted = $2, model = $3, updated_at = NOW()
		WHERE installation_id = $4
	`, provider, encrypted, model, installationID)
	return err
}

// DecryptAPIKey decrypts the stored API key. Returns "" for installations
// that have not configured a key.
func (s *Store) DecryptAPIKey(inst *Installation) (string, error) {
	if inst.APIKeyEncrypted == "" {
		return "", nil
	}
	return s.decrypt(inst.APIKeyEncrypted)
}

// ─── Crypto helpers ───────────────────────────────────────────────────────────

func (s *Store) encrypt(plaintext string) (string, error) {
	if s.gcm == nil {
		return "", errors.New("ENCRYPTION_KEY is not set — cannot store API keys")
	}
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := s.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *Store) decrypt(encoded string) (string, error) {
	if s.gcm == nil {
		return "", errors.New("ENCRYPTION_KEY is not set — cannot decrypt API keys")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	ns := s.gcm.NonceSize()
	if len(data) < ns {
		return "", errors.New("ciphertext too short")
	}
	plaintext, err := s.gcm.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("AES-GCM decrypt: %w", err)
	}
	return string(plaintext), nil
}

const defaultFreeLimit = 100
