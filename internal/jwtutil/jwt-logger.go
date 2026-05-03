package jwtutil

import (
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// JWTValidatorWithLogger расширяет JWTValidator логгером
type JWTValidatorWithLogger struct {
	*JWTValidator
	logger *zap.Logger
}

// NewJWTValidatorWithLogger создает валидатор с логгером
func NewJWTValidatorWithLogger(
	secretKey, algorithm string,
	validateExp, validateIss bool, expectedIss string,
	validateAud bool, expectedAud string,
	publicKeyFile string,
	logger *zap.Logger,
) (*JWTValidatorWithLogger, error) {

	validator, err := NewJWTValidator(
		secretKey, algorithm,
		validateExp, validateIss, expectedIss,
		validateAud, expectedAud,
		publicKeyFile,
	)
	if err != nil {
		return nil, err
	}

	return &JWTValidatorWithLogger{
		JWTValidator: validator,
		logger:       logger,
	}, nil
}

// ParseAndValidateWithLogging парсит и валидирует с логированием
func (v *JWTValidatorWithLogger) ParseAndValidateWithLogging(tokenString string) (jwt.MapClaims, error) {
	claims, err := v.ParseAndValidate(tokenString)

	if err != nil {
		v.logger.Debug("JWT validation failed",
			zap.Error(err),
			zap.String("token_preview", tokenString[:min(20, len(tokenString))]),
		)
	} else {
		v.logger.Debug("JWT validation successful",
			zap.Any("claims", claims),
		)
	}

	return claims, err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
