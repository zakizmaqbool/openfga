package authorizer

import (
	"context"

	"google.golang.org/grpc"
)

type AuthorizerMiddleware grpc.UnaryServerInterceptor

type Authorizer interface {
	Authorized(ctx context.Context, req *AuthorizedRequest) (*AuthorizedResponse, error)
}

type AuthorizedRequest struct {
	StoreID  string
	Object   string
	Relation string
	Subject  string
}

type AuthorizedResponse struct {
	Allowed bool
}
