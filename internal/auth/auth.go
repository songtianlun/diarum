package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/songtianlun/diarum/internal/store"
)

const ContextUserKey = "diarum_user"

type Claims struct {
	UserID string `json:"id"`
	jwt.RegisteredClaims
}

type Service struct {
	store *store.Store
}

func NewService(store *store.Store) *Service {
	return &Service{store: store}
}

func (s *Service) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}

func (s *Service) VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *Service) IssueToken(user *store.User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: user.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(30 * 24 * time.Hour)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.store.AuthSecret)
}

func (s *Service) ParseToken(token string) (*store.User, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (any, error) {
		return s.store.AuthSecret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil || !parsed.Valid || claims.UserID == "" {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "The request requires valid authorization token.")
	}
	user, err := s.store.GetUserByID(claims.UserID)
	if err != nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "The request requires valid authorization token.")
	}
	return user, nil
}

func (s *Service) Middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		header := c.Request().Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			return echo.NewHTTPError(http.StatusUnauthorized, "The request requires valid authorization token.")
		}
		user, err := s.ParseToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			return err
		}
		c.Set(ContextUserKey, user)
		return next(c)
	}
}

func CurrentUser(c echo.Context) *store.User {
	user, _ := c.Get(ContextUserKey).(*store.User)
	return user
}
