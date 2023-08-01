// Package requestid contains middleware to log the request ID.
package requestid

import (
	"context"

	"github.com/google/uuid"
	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	requestIDCtxKey = "request-id"
	requestIDHeader = "x-request-id"
)

// NewUnaryInterceptor creates a grpc.UnaryServerInterceptor which must
// come after the trace interceptor and before the logging interceptor.
// If tracing is enabled, request ID is set to be the trace ID.
// If tracing is disabled, request ID is a random UUID.
func NewUnaryInterceptor() grpc.UnaryServerInterceptor {
	return interceptors.UnaryServerInterceptor(reportable())
}

// NewStreamingInterceptor creates a grpc.StreamServerInterceptor which must
// come after the trace interceptor and before the logging interceptor.
// If tracing is enabled, request ID is set to be the trace ID.
// If tracing is disabled, request ID is a random UUID.
func NewStreamingInterceptor() grpc.StreamServerInterceptor {
	return interceptors.StreamServerInterceptor(reportable())
}

func reportable() interceptors.CommonReportableFunc {
	return func(ctx context.Context, c interceptors.CallMeta) (interceptors.Reporter, context.Context) {
		spanCtx := trace.SpanContextFromContext(ctx)

		requestID := ""
		if !spanCtx.TraceID().IsValid() {
			// If trace was not enabled, we will need to generate our own ULID
			id, _ := uuid.NewRandom()
			requestID = id.String()
		} else {
			requestID = spanCtx.TraceID().String()
		}

		// Add the requestID to the context tags
		grpc_ctxtags.Extract(ctx).Set(requestIDCtxKey, requestID)

		// Add the requestID to the response headers
		_ = grpc.SetHeader(ctx, metadata.Pairs(requestIDHeader, requestID))

		return interceptors.NoopReporter{}, ctx
	}
}
