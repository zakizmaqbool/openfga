package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/go-errors/errors"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	httpmiddleware "github.com/openfga/openfga/internal/middleware/http"
	"github.com/openfga/openfga/pkg/encoder"
	"github.com/openfga/openfga/pkg/logger"
	"github.com/openfga/openfga/pkg/utils/grpcutils"
	"github.com/openfga/openfga/server/commands"
	serverErrors "github.com/openfga/openfga/server/errors"
	"github.com/openfga/openfga/server/queries"
	"github.com/openfga/openfga/storage"
	"github.com/rs/cors"
	openfgav1pb "go.buf.build/openfga/go/openfga/api/openfga/v1"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	AuthorizationModelIdHeader = "openfga-authorization-model-id"
)

var (
	ErrNilTokenEncoder error = fmt.Errorf("TokenEncoder must be a non-nil interface value")
)

// A Server implements the OpenFGA service backend as both
// a GRPC and HTTP server.
type Server struct {
	openfgav1pb.UnimplementedOpenFGAServiceServer
	*grpc.Server
	tracer                    trace.Tracer
	meter                     metric.Meter
	logger                    logger.Logger
	typeDefinitionReadBackend storage.TypeDefinitionReadBackend
	authorizationModelBackend storage.AuthorizationModelBackend
	tupleBackend              storage.TupleBackend
	assertionsBackend         storage.AssertionsBackend
	changelogBackend          storage.ChangelogBackend
	storesBackend             storage.StoresBackend
	encoder                   encoder.Encoder
	config                    *Config

	defaultServeMuxOpts []runtime.ServeMuxOption
}

type Dependencies struct {
	AuthorizationModelBackend storage.AuthorizationModelBackend
	TypeDefinitionReadBackend storage.TypeDefinitionReadBackend
	TupleBackend              storage.TupleBackend
	ChangelogBackend          storage.ChangelogBackend
	AssertionsBackend         storage.AssertionsBackend
	StoresBackend             storage.StoresBackend
	Tracer                    trace.Tracer
	Meter                     metric.Meter
	Logger                    logger.Logger

	// TokenEncoder is the encoder used to encode continuation tokens for paginated views.
	// Defaults to Base64Encoder if none is provided.
	TokenEncoder encoder.Encoder
}

type Config struct {
	ServiceName            string
	RpcPort                int
	HttpPort               int
	ResolveNodeLimit       uint32
	ChangelogHorizonOffset int
	UnaryInterceptors      []grpc.UnaryServerInterceptor
	MuxOptions             []runtime.ServeMuxOption
}

// New creates a new Server which uses the supplied backends
// for managing data.
func New(dependencies *Dependencies, config *Config) (*Server, error) {
	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(config.UnaryInterceptors...))

	tokenEncoder := dependencies.TokenEncoder
	if tokenEncoder == nil {
		tokenEncoder = encoder.NewBase64Encoder()
	} else {
		t := reflect.TypeOf(tokenEncoder)
		if reflect.ValueOf(tokenEncoder) == reflect.Zero(t) {
			return nil, ErrNilTokenEncoder
		}
	}

	server := &Server{
		Server:                    grpcServer,
		tracer:                    dependencies.Tracer,
		meter:                     dependencies.Meter,
		logger:                    dependencies.Logger,
		authorizationModelBackend: dependencies.AuthorizationModelBackend,
		typeDefinitionReadBackend: dependencies.TypeDefinitionReadBackend,
		tupleBackend:              dependencies.TupleBackend,
		assertionsBackend:         dependencies.AssertionsBackend,
		changelogBackend:          dependencies.ChangelogBackend,
		storesBackend:             dependencies.StoresBackend,
		encoder:                   tokenEncoder,
		config:                    config,
		defaultServeMuxOpts: []runtime.ServeMuxOption{
			runtime.WithForwardResponseOption(httpmiddleware.HttpResponseModifier),

			runtime.WithErrorHandler(func(c context.Context, sr *runtime.ServeMux, mm runtime.Marshaler, w http.ResponseWriter, r *http.Request, e error) {
				actualCode := serverErrors.ConvertToEncodedErrorCode(status.Convert(e))
				if serverErrors.IsValidEncodedError(actualCode) {
					dependencies.Logger.ErrorWithContext(c, "gRPC error", logger.Error(e), logger.String("request_url", r.URL.String()))
				}

				httpmiddleware.CustomHTTPErrorHandler(c, w, r, serverErrors.NewEncodedError(actualCode, e.Error()))
			}),
		},
	}

	openfgav1pb.RegisterOpenFGAServiceServer(grpcServer, server)

	errors.MaxStackDepth = logger.MaxDepthBacktraceStack

	return server, nil
}

func (s *Server) Read(ctx context.Context, req *openfgav1pb.ReadRequest) (*openfgav1pb.ReadResponse, error) {
	store := req.GetStoreId()
	tk := req.GetTupleKey()
	ctx, span := s.tracer.Start(ctx, "read", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(store)},
		attribute.KeyValue{Key: "object", Value: attribute.StringValue(tk.GetObject())},
		attribute.KeyValue{Key: "relation", Value: attribute.StringValue(tk.GetRelation())},
		attribute.KeyValue{Key: "user", Value: attribute.StringValue(tk.GetUser())},
	))
	defer span.End()

	modelID, err := s.resolveAuthorizationModelId(ctx, store, req.GetAuthorizationModelId())
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.KeyValue{Key: "authorization-model-id", Value: attribute.StringValue(modelID)})

	q := queries.NewReadQuery(s.tupleBackend, s.typeDefinitionReadBackend, s.tracer, s.logger, s.encoder)
	return q.Execute(ctx, &openfgav1pb.ReadRequest{
		StoreId:              store,
		TupleKey:             tk,
		AuthorizationModelId: modelID,
		PageSize:             req.GetPageSize(),
		ContinuationToken:    req.GetContinuationToken(),
	})
}

func (s *Server) ReadTuples(ctx context.Context, readTuplesRequest *openfgav1pb.ReadTuplesRequest) (*openfgav1pb.ReadTuplesResponse, error) {

	ctx, span := s.tracer.Start(ctx, "readTuples", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(readTuplesRequest.GetStoreId())},
	))
	defer span.End()

	q := queries.NewReadTuplesQuery(s.tupleBackend, s.encoder, s.logger)
	return q.Execute(ctx, readTuplesRequest)
}

func (s *Server) Write(ctx context.Context, req *openfgav1pb.WriteRequest) (*openfgav1pb.WriteResponse, error) {
	store := req.GetStoreId()
	ctx, span := s.tracer.Start(ctx, "write", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(store)},
	))
	defer span.End()

	modelID, err := s.resolveAuthorizationModelId(ctx, store, req.GetAuthorizationModelId())
	if err != nil {
		return nil, err
	}

	cmd := commands.NewWriteCommand(s.tupleBackend, s.typeDefinitionReadBackend, s.tracer, s.logger)
	return cmd.Execute(ctx, &openfgav1pb.WriteRequest{
		StoreId:              store,
		AuthorizationModelId: modelID,
		Writes:               req.GetWrites(),
		Deletes:              req.GetDeletes(),
	})
}

func (s *Server) Check(ctx context.Context, req *openfgav1pb.CheckRequest) (*openfgav1pb.CheckResponse, error) {
	store := req.GetStoreId()
	tk := req.GetTupleKey()
	ctx, span := s.tracer.Start(ctx, "check", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(req.GetStoreId())},
		attribute.KeyValue{Key: "object", Value: attribute.StringValue(tk.GetObject())},
		attribute.KeyValue{Key: "relation", Value: attribute.StringValue(tk.GetRelation())},
		attribute.KeyValue{Key: "user", Value: attribute.StringValue(tk.GetUser())},
	))
	defer span.End()

	modelID, err := s.resolveAuthorizationModelId(ctx, store, req.GetAuthorizationModelId())
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.KeyValue{Key: "authorization-model-id", Value: attribute.StringValue(modelID)})

	q := queries.NewCheckQuery(s.tupleBackend, s.typeDefinitionReadBackend, s.tracer, s.meter, s.logger, s.config.ResolveNodeLimit)

	res, err := q.Execute(ctx, &openfgav1pb.CheckRequest{
		StoreId:              store,
		TupleKey:             tk,
		ContextualTuples:     req.GetContextualTuples(),
		AuthorizationModelId: modelID,
		Trace:                req.GetTrace(),
	})
	if err != nil {
		return nil, err
	}

	span.SetAttributes(attribute.KeyValue{Key: "allowed", Value: attribute.BoolValue(res.GetAllowed())})
	return res, nil
}

func (s *Server) Expand(ctx context.Context, req *openfgav1pb.ExpandRequest) (*openfgav1pb.ExpandResponse, error) {
	store := req.GetStoreId()
	tk := req.GetTupleKey()
	ctx, span := s.tracer.Start(ctx, "expand", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(store)},
		attribute.KeyValue{Key: "object", Value: attribute.StringValue(tk.GetObject())},
		attribute.KeyValue{Key: "relation", Value: attribute.StringValue(tk.GetRelation())},
		attribute.KeyValue{Key: "user", Value: attribute.StringValue(tk.GetUser())},
	))
	defer span.End()

	modelID, err := s.resolveAuthorizationModelId(ctx, store, req.GetAuthorizationModelId())
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.KeyValue{Key: "authorization-model-id", Value: attribute.StringValue(modelID)})

	q := queries.NewExpandQuery(s.tupleBackend, s.typeDefinitionReadBackend, s.tracer, s.logger)
	return q.Execute(ctx, &openfgav1pb.ExpandRequest{
		StoreId:              store,
		AuthorizationModelId: modelID,
		TupleKey:             tk,
	})
}

func (s *Server) ReadAuthorizationModel(ctx context.Context, req *openfgav1pb.ReadAuthorizationModelRequest) (*openfgav1pb.ReadAuthorizationModelResponse, error) {
	ctx, span := s.tracer.Start(ctx, "readAuthorizationModel", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(req.GetStoreId())},
		attribute.KeyValue{Key: "authorization-model-id", Value: attribute.StringValue(req.GetId())},
	))
	defer span.End()

	q := queries.NewReadAuthorizationModelQuery(s.authorizationModelBackend, s.logger)
	return q.Execute(ctx, req)
}

func (s *Server) WriteAuthorizationModel(ctx context.Context, req *openfgav1pb.WriteAuthorizationModelRequest) (*openfgav1pb.WriteAuthorizationModelResponse, error) {
	ctx, span := s.tracer.Start(ctx, "writeAuthorizationModel", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(req.GetStoreId())},
	))
	defer span.End()

	c := commands.NewWriteAuthorizationModelCommand(s.authorizationModelBackend, s.logger)
	return c.Execute(ctx, req)
}

func (s *Server) ReadAuthorizationModels(ctx context.Context, req *openfgav1pb.ReadAuthorizationModelsRequest) (*openfgav1pb.ReadAuthorizationModelsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "readAuthorizationModels", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(req.GetStoreId())},
	))
	defer span.End()

	c := queries.NewReadAuthorizationModelsQuery(s.authorizationModelBackend, s.encoder, s.logger)
	return c.Execute(ctx, req)
}

func (s *Server) WriteAssertions(ctx context.Context, req *openfgav1pb.WriteAssertionsRequest) (*openfgav1pb.WriteAssertionsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "writeAssertions", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(req.GetStoreId())},
	))
	defer span.End()
	authorizationModelId, err := s.resolveAuthorizationModelId(ctx, req.GetStoreId(), req.GetAuthorizationModelId())
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.KeyValue{Key: "authorization-model-id", Value: attribute.StringValue(authorizationModelId)})
	c := commands.NewWriteAssertionsCommand(s.assertionsBackend, s.typeDefinitionReadBackend, s.logger)
	return c.Execute(ctx, req)
}

func (s *Server) ReadAssertions(ctx context.Context, req *openfgav1pb.ReadAssertionsRequest) (*openfgav1pb.ReadAssertionsResponse, error) {
	ctx, span := s.tracer.Start(ctx, "readAssertions", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(req.GetStoreId())},
	))
	defer span.End()
	authorizationModelId, err := s.resolveAuthorizationModelId(ctx, req.GetStoreId(), req.GetAuthorizationModelId())
	if err != nil {
		return nil, err
	}
	span.SetAttributes(attribute.KeyValue{Key: "authorization-model-id", Value: attribute.StringValue(authorizationModelId)})
	q := queries.NewReadAssertionsQuery(s.assertionsBackend, s.logger)
	return q.Execute(ctx, req.GetStoreId(), req.GetAuthorizationModelId())
}

func (s *Server) ReadChanges(ctx context.Context, req *openfgav1pb.ReadChangesRequest) (*openfgav1pb.ReadChangesResponse, error) {
	ctx, span := s.tracer.Start(ctx, "ReadChangesQuery", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(req.GetStoreId())},
		attribute.KeyValue{Key: "type", Value: attribute.StringValue(req.GetType())},
	))
	defer span.End()

	q := queries.NewReadChangesQuery(s.changelogBackend, s.tracer, s.logger, s.encoder, s.config.ChangelogHorizonOffset)
	return q.Execute(ctx, req)
}

func (s *Server) CreateStore(ctx context.Context, req *openfgav1pb.CreateStoreRequest) (*openfgav1pb.CreateStoreResponse, error) {
	ctx, span := s.tracer.Start(ctx, "createStore")
	defer span.End()

	c := commands.NewCreateStoreCommand(s.storesBackend, s.logger)
	response, err := c.Execute(ctx, req)
	if err != nil {
		return nil, err
	}
	grpcutils.SetHeaderLogError(ctx, httpmiddleware.XHttpCode, strconv.Itoa(http.StatusCreated), s.logger)

	return response, nil
}

func (s *Server) DeleteStore(ctx context.Context, req *openfgav1pb.DeleteStoreRequest) (*openfgav1pb.DeleteStoreResponse, error) {
	ctx, span := s.tracer.Start(ctx, "deleteStore")
	defer span.End()

	cmd := commands.NewDeleteStoreCommand(s.storesBackend, s.logger)
	err := cmd.Execute(ctx, req)
	if err != nil {
		return nil, err
	}
	grpcutils.SetHeaderLogError(ctx, httpmiddleware.XHttpCode, strconv.Itoa(http.StatusNoContent), s.logger)

	return &openfgav1pb.DeleteStoreResponse{}, nil
}

func (s *Server) GetStore(ctx context.Context, req *openfgav1pb.GetStoreRequest) (*openfgav1pb.GetStoreResponse, error) {
	ctx, span := s.tracer.Start(ctx, "getStore", trace.WithAttributes(
		attribute.KeyValue{Key: "store", Value: attribute.StringValue(req.GetStoreId())},
	))
	defer span.End()

	q := queries.NewGetStoreQuery(s.storesBackend, s.logger)
	return q.Execute(ctx, req)
}

func (s *Server) ListStores(ctx context.Context, req *openfgav1pb.ListStoresRequest) (*openfgav1pb.ListStoresResponse, error) {
	ctx, span := s.tracer.Start(ctx, "listStores")
	defer span.End()

	q := queries.NewListStoresQuery(s.storesBackend, s.encoder, s.logger)
	return q.Execute(ctx, req)
}

// Run starts server execution, and blocks until complete, returning any serverErrors.
func (s *Server) Run(ctx context.Context) error {
	rpcAddr := fmt.Sprintf("localhost:%d", s.config.RpcPort)
	lis, err := net.Listen("tcp", rpcAddr)
	if err != nil {
		return err
	}

	go func() {
		if err := s.Serve(lis); err != nil {
			s.logger.Error("failed to start grpc server", logger.Error(err))
		}
	}()

	s.logger.Info(fmt.Sprintf("GRPC Server listening on %s", rpcAddr))

	muxOpts := []runtime.ServeMuxOption{}
	muxOpts = append(muxOpts, s.defaultServeMuxOpts...) // register the defaults first
	muxOpts = append(muxOpts, s.config.MuxOptions...)   // any provided options override defaults if they are duplicates

	mux := runtime.NewServeMux(muxOpts...)

	if err := openfgav1pb.RegisterOpenFGAServiceHandlerFromEndpoint(ctx, mux, rpcAddr, []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(), grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
	}); err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr: fmt.Sprintf(":%d", s.config.HttpPort),
		Handler: cors.New(cors.Options{
			AllowedOrigins:   []string{"*"},
			AllowCredentials: true,
			AllowedHeaders:   []string{"*"},
			AllowedMethods: []string{http.MethodGet, http.MethodPost,
				http.MethodHead, http.MethodPatch, http.MethodDelete, http.MethodPut},
		}).Handler(mux),
	}

	httpServer.RegisterOnShutdown(func() {
		s.Stop()
	})

	go func() {
		s.logger.Info(fmt.Sprintf("HTTP Server listening on %s", httpServer.Addr))
		err := httpServer.ListenAndServe()

		if err != http.ErrServerClosed {
			s.logger.ErrorWithContext(ctx, "HTTP Server closed with unexpected error", logger.Error(err))
		}
	}()

	<-ctx.Done()
	s.logger.InfoWithContext(ctx, "Termination signal received! Gracefully shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		s.logger.ErrorWithContext(ctx, "HTTP Server shutdown failed", logger.Error(err))
		return err
	}

	return nil
}

// Util to find the latest authorization model ID to be used through all the request lifecycle.
// This allows caching of types. If the user inserts a new authorization model and doesn't
// provide this field (which should be rate limited more aggressively) the in-flight requests won't be
// affected and newer calls will use the updated authorization model.
func (s *Server) resolveAuthorizationModelId(ctx context.Context, store, authorizationModelId string) (string, error) {
	var err error

	if authorizationModelId == "" {
		authorizationModelId, err = s.authorizationModelBackend.FindLatestAuthorizationModelID(ctx, store)
		if err != nil {
			if errors.Is(err, storage.NotFound) {
				return "", serverErrors.LatestAuthorizationModelNotFound(store)
			}
			return "", serverErrors.HandleError("", err)
		}
	}

	if _, err := s.authorizationModelBackend.ReadAuthorizationModel(ctx, store, authorizationModelId); err != nil {
		if errors.Is(err, storage.NotFound) {
			return "", serverErrors.AuthorizationModelNotFound(authorizationModelId)
		}
		return "", serverErrors.HandleError("", err)
	}

	grpcutils.SetHeaderLogError(ctx, AuthorizationModelIdHeader, authorizationModelId, s.logger)

	return authorizationModelId, nil
}
