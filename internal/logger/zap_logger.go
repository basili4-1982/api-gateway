package logger

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/basili4-1982/api-gateway/internal/config"
)

// NewZapLogger создает новый zap логгер на основе конфигурации
func NewZapLogger(cfg *config.LoggingConfig) (*zap.Logger, error) {
	// Настройка уровня логирования
	var level zapcore.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	case "panic":
		level = zapcore.PanicLevel
	case "fatal":
		level = zapcore.FatalLevel
	default:
		level = zapcore.InfoLevel
	}

	// Настройка энкодера
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder, // INFO, ERROR и т.д.
		EncodeTime:     zapcore.ISO8601TimeEncoder,  // 2024-01-02T15:04:05Z
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder, // путь/файл:строка
	}

	// Выбор формата (JSON или консоль)
	var encoder zapcore.Encoder
	if cfg.Format == "json" {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		// Для консольного вывода добавляем цвета
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	// Создаем ядро
	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(os.Stdout),
		level,
	)

	// Создаем логгер с добавлением caller
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return logger, nil
}

// MustZapLogger создает логгер или паникует при ошибке
func MustZapLogger(cfg *config.LoggingConfig) *zap.Logger {
	logger, err := NewZapLogger(cfg)
	if err != nil {
		panic("failed to create logger: " + err.Error())
	}
	return logger
}
