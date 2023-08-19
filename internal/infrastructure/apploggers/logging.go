package apploggers

import "go.uber.org/zap"

type AppLogger interface {
	GetLogger() *zap.Logger
}
