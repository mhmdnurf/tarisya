package core

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
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
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func passwordMatches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
