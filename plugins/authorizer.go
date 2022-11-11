package main

import (
	"context"
	"fmt"
	"log"

	"github.com/openfga/openfga/pkg/authorizer"
	"github.com/openfga/openfga/pkg/plugin"
	"github.com/openfga/openfga/server/authn"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	openfgapb "go.buf.build/openfga/go/openfga/api/openfga/v1"
)

type StoreScopedRequest interface {
	GetStoreId() string
}

func InitPlugin(pm *plugin.PluginManager) error {

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	conn, err := grpc.Dial("0.0.0.0:8081", opts...)
	if err != nil {
		return fmt.Errorf("failed to establish grpc connection with OpenFGA server: %w", err)
	}
	// defer conn.Close() somehow make this close on plugin closure

	client := openfgapb.NewOpenFGAServiceClient(conn)

	authorizer := &OpenFGAAuthorizer{
		client,
	}

	pm.RegisterAuthorizerMiddleware(authorizer.AuthorizerMiddlewareFunc())

	return nil
}

type Checker interface {
	Check(ctx context.Context, req *openfgapb.CheckRequest) (*openfgapb.CheckResponse, error)
}

// OpenFGAAuthorizer implements the authorizer.Authorized interface in a way that delegates
// authorization decisions to OpenFGA (using OpenFGA to enforce authorization for OpenFGA).
type OpenFGAAuthorizer struct {
	openfgapb.OpenFGAServiceClient
}

func (a *OpenFGAAuthorizer) Authorized(ctx context.Context, req *authorizer.AuthorizedRequest) (*authorizer.AuthorizedResponse, error) {
	resp, err := a.Check(ctx, &openfgapb.CheckRequest{
		StoreId: req.StoreID,
		TupleKey: &openfgapb.TupleKey{
			Object:   req.Object,
			Relation: req.Relation,
			User:     req.Subject,
		},
	})
	if err != nil {
		return &authorizer.AuthorizedResponse{Allowed: false}, err
	}

	if resp.Allowed {
		return &authorizer.AuthorizedResponse{Allowed: true}, nil
	}

	// deny by default
	return &authorizer.AuthorizedResponse{Allowed: false}, nil
}

func (a *OpenFGAAuthorizer) AuthorizerMiddlewareFunc() authorizer.AuthorizerMiddleware {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		claims, ok := authn.AuthClaimsFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		_ = claims

		var s StoreScopedRequest

		var object string
		var relation string

		switch info.FullMethod {
		case "/openfga.v1.OpenFGAService/CreateStore":
			object = "_openfga:root"
			relation = "create_store"
		case "/openfga.v1.OpenFGAService/Write":
			s = req.(*openfgapb.WriteRequest)
			relation = "tuple_writer"
		case "/openfga.v1.OpenFGAservice/Read":
			s = req.(*openfgapb.ReadRequest)
			relation = "tuple_reader"
		default:
			return handler(ctx, req)
		}

		//subject := fmt.Sprintf("subject:%s", claims.Subject)
		subject := "subject:jon"

		storeID := "01GHJ1WH1MXDRA89XK2JXEP1RM"
		if s != nil {
			object = fmt.Sprintf("store:%s", storeID)
		}

		resp, err := a.Authorized(ctx, &authorizer.AuthorizedRequest{
			StoreID:  storeID,
			Object:   object,
			Relation: relation,
			Subject:  subject,
		})
		if err != nil {
			// todo: log error
			log.Fatal(err)

			// deny by default
			return nil, status.Error(codes.PermissionDenied, "unauthorized")
		}

		if resp.Allowed {
			return handler(ctx, req)
		}

		return nil, status.Error(codes.PermissionDenied, "unauthorized")
	}
}
