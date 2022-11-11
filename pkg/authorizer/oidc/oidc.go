package oidc

import (
	"context"

	"github.com/openfga/openfga/server/authn"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OIDCScopeAuthorizerFunc implements an AuthorizerFunc that enforces endpoint-level authorization based on OIDC
// scopes.
func OIDCScopeAuthorizerFunc() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {

		claims, ok := authn.AuthClaimsFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}

		var requiredScope string

		switch info.FullMethod {
		case "openfga.v1.OpenFGAService/Write":
			requiredScope = "openfga:write"
		case "openfga.v1.OpenFGAservice/Read":
			requiredScope = "openfga:read"

		default:
			return nil, status.Error(codes.PermissionDenied, "unauthorized")
		}

		for scope := range claims.Scopes {
			if scope == requiredScope {
				return handler(ctx, req)
			}
		}

		return nil, status.Error(codes.PermissionDenied, "unauthorized")
	}
}
