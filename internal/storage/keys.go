package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// CommentKey uniquely identifies a posted inline comment on a pull request.
// Used to skip re-posting the same finding on later synchronize events.
type CommentKey struct {
	Path     string
	Line     int
	Category string
}

// HashCommentBody returns a stable hex SHA-256 of the comment text.
func HashCommentBody(body string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(body)))
	return hex.EncodeToString(sum[:])
}
