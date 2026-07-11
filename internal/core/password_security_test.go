package core

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPasswordUsesArgon2id(t *testing.T) {
	const password = "correct horse battery staple"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("password hash has unexpected format %q", hash)
	}
	if matches, needsRehash := verifyPassword(hash, password); !matches || needsRehash {
		t.Fatalf("verifyPassword returned matches=%v needsRehash=%v, want true and false", matches, needsRehash)
	}
	if matches, _ := verifyPassword(hash, "wrong password"); matches {
		t.Fatal("wrong password matched Argon2id hash")
	}

	secondHash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if secondHash == hash {
		t.Fatal("two password hashes used the same salt")
	}
}

func TestVerifyPasswordSupportsBcryptAndRequestsUpgrade(t *testing.T) {
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte("legacy-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	matches, needsRehash := verifyPassword(string(bcryptHash), "legacy-password")
	if !matches || !needsRehash {
		t.Fatalf("verifyPassword returned matches=%v needsRehash=%v, want true and true", matches, needsRehash)
	}
	if matches, _ := verifyPassword(string(bcryptHash), "wrong-password"); matches {
		t.Fatal("wrong password matched bcrypt hash")
	}
}

func TestVerifyPasswordRejectsUnsafeArgon2idParameters(t *testing.T) {
	hashes := []string{
		"$argon2id$v=19$m=999999,t=3,p=2$c2FsdHNhbHQ$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=65536,t=99,p=2$c2FsdHNhbHQ$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=65536,t=3,p=99$c2FsdHNhbHQ$YWJjZGVmZ2hpamtsbW5vcA",
		"not-a-password-hash",
	}
	for _, hash := range hashes {
		if matches, needsRehash := verifyPassword(hash, "password"); matches || needsRehash {
			t.Fatalf("unsafe hash %q returned matches=%v needsRehash=%v", hash, matches, needsRehash)
		}
	}
}

func TestVerifyPasswordRequestsUpgradeForOldArgon2idParameters(t *testing.T) {
	params := currentArgon2idParameters
	params.memory /= 2
	hash := encodeArgon2id("password", []byte("0123456789abcdef"), params)

	matches, needsRehash := verifyPassword(hash, "password")
	if !matches || !needsRehash {
		t.Fatalf("verifyPassword returned matches=%v needsRehash=%v, want true and true", matches, needsRehash)
	}
}

func TestLoginUpgradesBcryptHashToArgon2id(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte("legacy-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, "Legacy User", "legacy@example.com", string(bcryptHash))
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(store, Config{
		JWTSecret:     testJWTSecret,
		JWTExpiration: time.Hour,
	})
	body, err := json.Marshal(authRequest{Email: user.Email, Password: "legacy-password"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	upgraded, err := store.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(upgraded.PasswordHash, "$argon2id$") {
		t.Fatalf("upgraded password hash = %q, want Argon2id", upgraded.PasswordHash)
	}
	if upgraded.PasswordHash == string(bcryptHash) {
		t.Fatal("bcrypt password hash was not replaced")
	}
	if matches, needsRehash := verifyPassword(upgraded.PasswordHash, "legacy-password"); !matches || needsRehash {
		t.Fatalf("upgraded hash verification matches=%v needsRehash=%v", matches, needsRehash)
	}
}
