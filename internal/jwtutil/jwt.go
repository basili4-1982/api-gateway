package jwtutil

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrNoToken          = errors.New("no token provided")
	ErrInvalidToken     = errors.New("invalid token")
	ErrInvalidAlgorithm = errors.New("invalid signing algorithm")
)

// JWTValidator валидатор JWT токенов
type JWTValidator struct {
	secretKey   []byte
	algorithm   string
	validateExp bool
	validateIss bool
	expectedIss string
	validateAud bool
	expectedAud string
}

// NewJWTValidator создает новый валидатор JWT
func NewJWTValidator(secretKey, algorithm string,
	validateExp, validateIss bool, expectedIss string,
	validateAud bool, expectedAud string) (*JWTValidator, error) {

	v := &JWTValidator{
		algorithm:   algorithm,
		validateExp: validateExp,
		validateIss: validateIss,
		expectedIss: expectedIss,
		validateAud: validateAud,
		expectedAud: expectedAud,
	}

	// Определяем тип ключа на основе алгоритма
	if strings.HasPrefix(algorithm, "HS") {
		// HMAC - симметричный ключ
		if secretKey == "" {
			return nil, fmt.Errorf("secret key required for HMAC algorithm")
		}
		v.secretKey = []byte(secretKey)
	} else {
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}

	return v, nil
}

// ParseAndValidate парсит и валидирует JWT токен
func (v *JWTValidator) ParseAndValidate(tokenString string) (jwt.MapClaims, error) {
	if tokenString == "" {
		return nil, ErrNoToken
	}

	// Убираем префикс "Bearer " если есть
	tokenString = strings.TrimPrefix(tokenString, "Bearer ")

	// Парсим токен
	token, err := jwt.Parse(tokenString, v.keyFunc, v.withValidOptions()...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// keyFunc возвращает ключ для проверки подписи
func (v *JWTValidator) keyFunc(token *jwt.Token) (interface{}, error) {
	// Проверяем алгоритм
	if token.Method.Alg() != v.algorithm {
		return nil, fmt.Errorf("%w: expected %s, got %s",
			ErrInvalidAlgorithm, v.algorithm, token.Method.Alg())
	}

	// Возвращаем соответствующий ключ
	if v.secretKey != nil {
		return v.secretKey, nil
	}
	return nil, fmt.Errorf("no key available")
}

// withValidOptions возвращает опции валидации
func (v *JWTValidator) withValidOptions() []jwt.ParserOption {
	var opts []jwt.ParserOption

	if !v.validateExp {
		opts = append(opts, jwt.WithoutClaimsValidation())
	}

	return opts
}

// ValidateClaims выполняет дополнительную валидацию claims
func (v *JWTValidator) ValidateClaims(claims jwt.MapClaims) error {
	// Проверка issuer
	if v.validateIss {
		iss, ok := claims["iss"].(string)
		if !ok {
			return fmt.Errorf("iss claim missing")
		}
		if iss != v.expectedIss {
			return fmt.Errorf("invalid issuer: expected %s, got %s", v.expectedIss, iss)
		}
	}

	// Проверка audience
	if v.validateAud {
		aud, ok := claims["aud"].(string)
		if !ok {
			// Может быть массивом строк
			if audSlice, claimsOk := claims["aud"].([]interface{}); claimsOk && len(audSlice) > 0 {
				aud, ok = audSlice[0].(string)
			}
		}
		if !ok || aud != v.expectedAud {
			return fmt.Errorf("invalid audience")
		}
	}

	return nil
}

// ExtractClaims извлекает значения из claims
func ExtractClaims(claims jwt.MapClaims, mappings []string) map[string]interface{} {
	result := make(map[string]interface{})

	for _, key := range mappings {
		if val, ok := claims[key]; ok {
			result[key] = val
		}
	}

	return result
}
