package storage

import "testing"

func TestCommentKey_Identity(t *testing.T) {
	k := CommentKey{Path: "internal/foo.go", Line: 12, Category: "bug"}
	if k.Path != "internal/foo.go" || k.Line != 12 || k.Category != "bug" {
		t.Fatalf("unexpected key: %+v", k)
	}
}

func TestHashCommentBody_Stable(t *testing.T) {
	a := HashCommentBody("nil deref — add a check")
	b := HashCommentBody("nil deref — add a check")
	if a == "" {
		t.Fatal("hash should not be empty")
	}
	if a != b {
		t.Fatalf("same body should hash the same: %q vs %q", a, b)
	}
}

func TestHashCommentBody_DifferentBodies(t *testing.T) {
	a := HashCommentBody("issue A")
	b := HashCommentBody("issue B")
	if a == b {
		t.Fatal("different bodies should not hash the same")
	}
}

func TestNewStore_EmptyEncryptionKey(t *testing.T) {
	s, err := NewStore(nil, "")
	if err != nil {
		t.Fatalf("empty encryption key should be allowed: %v", err)
	}
	if s.HasEncryption() {
		t.Fatal("HasEncryption should be false without a key")
	}
}
