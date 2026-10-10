package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/azure-local/azure-local/internal/gateway"
	"github.com/azure-local/azure-local/internal/kernel"
	"github.com/azure-local/azure-local/internal/providers/arm"
	"github.com/azure-local/azure-local/internal/providers/authorization"
	"github.com/azure-local/azure-local/internal/providers/blob"
	"github.com/azure-local/azure-local/internal/providers/identity"
)

type Server struct {
	cfg      kernel.Config
	logger   *slog.Logger
	registry *kernel.Registry
	bus      *kernel.EventBus
	persist  *kernel.PersistenceManager
	http     *http.Server

	identity      *identity.Provider
	authorization *authorization.Provider
}

func New(cfg kernel.Config, logger *slog.Logger) (*Server, error) {
	persist, err := kernel.NewPersistenceManager(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("persistence: %w", err)
	}
	bus := kernel.NewEventBus()
	registry := kernel.NewRegistry()

	idp, err := identity.New(persist, bus, logger)
	if err != nil {
		return nil, fmt.Errorf("identity provider: %w", err)
	}
	if err := registry.Register(idp); err != nil {
		return nil, fmt.Errorf("register identity: %w", err)
	}

	authz, err := authorization.New(persist, bus, logger,
		kernel.OIDFor("local-client"))
	if err != nil {
		return nil, fmt.Errorf("authorization provider: %w", err)
	}
	// RBAC enforcement defaults off so `curl` without a token still works.
	// Policy is always evaluated when assignments exist.
	authz.SetEnforceRBAC(false)
	if err := registry.Register(authz); err != nil {
		return nil, fmt.Errorf("register authorization: %w", err)
	}

	armProvider, err := arm.New(persist, bus, logger, nil)
	if err != nil {
		return nil, fmt.Errorf("arm provider: %w", err)
	}
	armProvider.SetAuthorizer(authz, func(req *http.Request) (string, error) {
		claims, err := idp.ExtractClaims(req)
		if err != nil {
			return "", err
		}
		oid, _ := claims["oid"].(string)
		if oid == "" {
			return "", fmt.Errorf("token has no oid claim")
		}
		return oid, nil
	}, false)
	if err := registry.Register(armProvider); err != nil {
		return nil, fmt.Errorf("register arm: %w", err)
	}

	blobProvider, err := blob.New(persist, bus, logger)
	if err != nil {
		return nil, fmt.Errorf("blob provider: %w", err)
	}
	if err := registry.Register(blobProvider); err != nil {
		return nil, fmt.Errorf("register blob: %w", err)
	}

	router := gateway.New(logger, registry)
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.BindAddress, cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return &Server{
		cfg:           cfg,
		logger:        logger,
		registry:      registry,
		bus:           bus,
		persist:       persist,
		http:          httpSrv,
		identity:      idp,
		authorization: authz,
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	for _, svc := range s.registry.All() {
		if err := svc.Init(ctx); err != nil {
			return fmt.Errorf("init %s: %w", svc.Name(), err)
		}
		if err := svc.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", svc.Name(), err)
		}
	}

	ln, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("Azure Local listening",
			"addr", ln.Addr().String(),
			"data", s.persist.Root(),
			"network", s.cfg.NetworkMode,
		)
		errCh <- s.http.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("http shutdown error", "err", err)
		}
		for _, svc := range s.registry.All() {
			if err := svc.Stop(shutdownCtx); err != nil {
				s.logger.Error("stop service", "name", svc.Name(), "err", err)
			}
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
