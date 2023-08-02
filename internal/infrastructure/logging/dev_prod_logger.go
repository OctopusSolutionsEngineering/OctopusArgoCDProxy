package logging

import (
	"go.uber.org/zap"
	"os"
	"strings"
)

type DevProdLogger struct {
	log *zap.Logger
}

func NewDevProdLogger() (*DevProdLogger, error) {
	logger := DevProdLogger{}

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

func (d *DevProdLogger) GetLogger() *zap.Logger {
	return d.log
}
