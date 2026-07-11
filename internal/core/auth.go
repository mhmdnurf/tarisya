package core

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	argon2idSaltLength  = 16
	argon2idKeyLength   = 32
	maxArgon2idMemory   = 128 * 1024
	maxArgon2idTime     = 6
	maxArgon2idThreads  = 8
	passwordWorkerCount = 2
)

type argon2idParameters struct {
	memory  uint32
	time    uint32
	threads uint8
	keyLen  uint32
}

var (
	currentArgon2idParameters = argon2idParameters{
		memory:  64 * 1024,
		time:    3,
		threads: 2,
		keyLen:  argon2idKeyLength,
	}
	passwordWorkers = make(chan struct{}, passwordWorkerCount)
)

type Auth struct {
	secret     []byte
	expiration time.Duration
}

type UserClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

func NewAuth(secret string, expiration time.Duration) *Auth {
	return &Auth{secret: []byte(secret), expiration: expiration}
}

func (a *Auth) Issue(user User) (string, error) {
	now := time.Now()
	claims := UserClaims{
		Email: user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(user.ID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(a.expiration)),
			Issuer:    "tarisya-core",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
}

func (a *Auth) Parse(tokenString string) (int64, error) {
	token, err := jwt.ParseWithClaims(tokenString, &UserClaims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return a.secret, nil
	}, jwt.WithIssuer("tarisya-core"), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return 0, errors.New("invalid or expired token")
	}
	claims, ok := token.Claims.(*UserClaims)
	if !ok {
		return 0, errors.New("invalid token claims")
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID < 1 {
		return 0, errors.New("invalid token subject")
	}
	return userID, nil
}

func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2idSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	return encodeArgon2id(password, salt, currentArgon2idParameters), nil
}

func passwordMatches(hash, password string) bool {
	matches, _ := verifyPassword(hash, password)
	return matches
}

// verifyPassword reports whether the password matches and whether a successful
// login should replace the stored hash with the current Argon2id parameters.
func verifyPassword(encodedHash, password string) (matches, needsRehash bool) {
	if strings.HasPrefix(encodedHash, "$2a$") || strings.HasPrefix(encodedHash, "$2b$") || strings.HasPrefix(encodedHash, "$2y$") {
		matches = bcrypt.CompareHashAndPassword([]byte(encodedHash), []byte(password)) == nil
		return matches, matches
	}

	params, salt, expected, err := parseArgon2id(encodedHash)
	if err != nil {
		return false, false
	}
	actual := deriveArgon2id([]byte(password), salt, params)
	matches = subtle.ConstantTimeCompare(actual, expected) == 1
	needsRehash = matches && (params != currentArgon2idParameters || len(salt) != argon2idSaltLength)
	return matches, needsRehash
}

func encodeArgon2id(password string, salt []byte, params argon2idParameters) string {
	hash := deriveArgon2id([]byte(password), salt, params)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		params.memory,
		params.time,
		params.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
}

func deriveArgon2id(password, salt []byte, params argon2idParameters) []byte {
	passwordWorkers <- struct{}{}
	defer func() { <-passwordWorkers }()
	return argon2.IDKey(password, salt, params.time, params.memory, params.threads, params.keyLen)
}

func parseArgon2id(encoded string) (argon2idParameters, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return argon2idParameters{}, nil, nil, errors.New("invalid Argon2id hash format")
	}
	params, err := parseArgon2idParameters(parts[3])
	if err != nil {
		return argon2idParameters{}, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return argon2idParameters{}, nil, nil, errors.New("invalid Argon2id salt")
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) < 16 || len(hash) > 64 {
		return argon2idParameters{}, nil, nil, errors.New("invalid Argon2id hash")
	}
	params.keyLen = uint32(len(hash))
	return params, salt, hash, nil
}

func parseArgon2idParameters(encoded string) (argon2idParameters, error) {
	values := strings.Split(encoded, ",")
	if len(values) != 3 {
		return argon2idParameters{}, errors.New("invalid Argon2id parameters")
	}
	memory, err := parseArgon2idParameter(values[0], "m=", maxArgon2idMemory)
	if err != nil {
		return argon2idParameters{}, err
	}
	iterations, err := parseArgon2idParameter(values[1], "t=", maxArgon2idTime)
	if err != nil {
		return argon2idParameters{}, err
	}
	threads, err := parseArgon2idParameter(values[2], "p=", maxArgon2idThreads)
	if err != nil || memory < 8*threads {
		return argon2idParameters{}, errors.New("invalid Argon2id parameters")
	}
	return argon2idParameters{memory: uint32(memory), time: uint32(iterations), threads: uint8(threads)}, nil
}

func parseArgon2idParameter(value, prefix string, maximum uint64) (uint64, error) {
	if !strings.HasPrefix(value, prefix) {
		return 0, errors.New("invalid Argon2id parameters")
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix), 10, 32)
	if err != nil || parsed == 0 || parsed > maximum {
		return 0, errors.New("invalid Argon2id parameters")
	}
	return parsed, nil
}
