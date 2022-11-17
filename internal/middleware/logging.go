package middleware

import (
	"context"
	"time"

	"github.com/openfga/openfga/pkg/logger"
	serverErrors "github.com/openfga/openfga/server/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

var (
	tookTimeCounter = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "took_time",
		Help: "The total time (in ms) that the request took",
	})
)

func NewLoggingInterceptor(logger logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		tookTime := time.Since(start)

		fields := []zap.Field{
			zap.String("method", info.FullMethod),
			zap.Duration("took", tookTime),
		}

		tookTimeCounter.Set(float64(tookTime.Milliseconds()))

		if err != nil {
			if internalError, ok := err.(serverErrors.InternalError); ok {
				fields = append(fields, zap.Error(internalError.Internal()))
			}

			fields = append(fields, zap.String("public_error", err.Error()))
			logger.Error("grpc_error", fields...)

			return nil, err
		}

		logger.Info("grpc_complete", fields...)

		return resp, nil
	}
}

func NewStreamingLoggingInterceptor(logger logger.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		err := handler(srv, stream)

		tookTime := time.Since(start)

		fields := []zap.Field{
			zap.String("method", info.FullMethod),
			zap.Duration("took", tookTime),
		}

		tookTimeCounter.Set(float64(tookTime.Milliseconds()))

		if err != nil {
			if internalError, ok := err.(serverErrors.InternalError); ok {
				fields = append(fields, zap.Error(internalError.Internal()))
			}

			fields = append(fields, zap.String("public_error", err.Error()))
			logger.Error("grpc_error", fields...)

			return err
		}

		logger.Info("grpc_complete", fields...)

		return nil
	}
}
