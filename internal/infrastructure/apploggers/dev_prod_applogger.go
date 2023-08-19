package apploggers

import (
	"go.uber.org/zap"
	"os"
	"strings"
)

// DevProdAppLogger provides apploggers facilities that can be configured via the APP_ENV environment variable
type DevProdAppLogger struct {
	log *zap.Logger
}

func NewDevProdLogger() (*DevProdAppLogger, error) {
	logger := DevProdAppLogger{}

	if strings.ToLower(os.Getenv("APP_ENV")) == "production" {
		log, err := zap.NewProduction()

		if err != nil {
			return nil, err
		}

		logger.log = log
	} else {
		log, err := zap.NewDevelopment()

		if err != nil {
			return nil, err
		}

		logger.log = log
	}

	return &logger, nil
}

func (d *DevProdAppLogger) GetLogger() *zap.Logger {
	return d.log
}
