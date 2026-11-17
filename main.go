package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/keymanagement"
)

var version = "dev"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		var status any
		var reqID any
		var serviceErr common.ServiceError
		if errors.As(err, &serviceErr) {
			status = serviceErr.GetHTTPStatusCode()
			reqID = serviceErr.GetOpcRequestID()
		}
		slog.Error("resolver failed", "stage", failureStage(err), "error_type", classify(err), "http_status", status, "request_id", reqID)
		os.Exit(1)
	}
}

func run(parent context.Context, args []string) error {
	fs := flag.NewFlagSet("oci-vault-resolver", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	showVersion := fs.Bool("version", false, "print version and target architecture")
	if err := fs.Parse(args); err != nil {
		return atStage("arguments", err)
	}
	if *showVersion && fs.NArg() == 0 {
		fmt.Printf("oci-vault-resolver %s %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
		return nil
	}
	if fs.NArg() != 0 {
		return atStage("arguments", errors.New("unexpected positional arguments"))
	}
	cfg, err := loadConfig()
	if err != nil {
		return atStage("configuration", err)
	}
	ctx, cancel := context.WithTimeout(parent, 180*time.Second)
	defer cancel()
	kube, err := newKubeClient()
	if err != nil {
		return atStage("kubernetes_client", err)
	}
	lease, err := NewKubernetesLease(kube, cfg.PodUID, cfg.Namespace)
	if err != nil {
		return atStage("lease_initialize", err)
	}
	if err := lease.Acquire(ctx); err != nil {
		return atStage("lease_acquire", err)
	}
	workCtx, workCancel := context.WithCancel(ctx)
	defer workCancel()
	go func() {
		lease.Renew(workCtx)
		workCancel()
	}()
	defer func() {
		if err := lease.Release(context.Background()); err != nil {
			slog.Warn("lease release failed", "stage", "lease_release", "error_type", classify(err))
		}
	}()
	provider, err := auth.InstancePrincipalConfigurationProviderForRegion(common.Region(cfg.Region))
	if err != nil {
		return atStage("oci_authentication", err)
	}
	client, err := keymanagement.NewKmsVaultClientWithConfigurationProvider(provider)
	if err != nil {
		return atStage("oci_client", err)
	}
	client.SetRegion(cfg.Region)
	vault, err := ResolveVault(workCtx, OCIListVaults{Client: client}, cfg.CompartmentID, cfg.VaultName)
	if err != nil {
		return atStage("vault_lookup", err)
	}
	if err := lease.AssertHeld(workCtx); err != nil {
		return atStage("lease_assert", err)
	}
	store := NewKubernetesStore(kube)
	if err := store.Ensure(workCtx, cfg, vault.ID, func(check context.Context) error { return lease.AssertHeld(check) }); err != nil {
		return atStage("secretstore_sync", err)
	}
	slog.Info("SecretStore synchronized", "vault_count", 1)
	return nil
}

type stagedError struct {
	stage string
	err   error
}

func (e stagedError) Error() string { return e.stage + " failed" }
func (e stagedError) Unwrap() error { return e.err }
func atStage(stage string, err error) error {
	if err == nil {
		return nil
	}
	return stagedError{stage, err}
}
func failureStage(err error) string {
	var e stagedError
	if errors.As(err, &e) {
		return e.stage
	}
	return "lease_release"
}

func classify(err error) string {
	var serviceErr common.ServiceError
	if errors.As(err, &serviceErr) {
		switch serviceErr.GetHTTPStatusCode() {
		case 401:
			return "authentication_error"
		case 403:
			return "authorization_error"
		case 429:
			return "rate_limited"
		default:
			if serviceErr.GetHTTPStatusCode() >= 500 {
				return "service_error"
			}
			return "oci_service_error"
		}
	}
	switch {
	case errors.Is(err, ErrNoVault):
		return "vault_not_found"
	case errors.Is(err, ErrMultipleVaults):
		return "vault_ambiguous"
	case errors.Is(err, ErrInvalidConfig):
		return "invalid_input"
	case errors.Is(err, ErrLeaseHeld), errors.Is(err, ErrLeaseLost):
		return "lease_conflict"
	case errors.Is(err, ErrStoreOwnership):
		return "store_ownership_conflict"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return "network_error"
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			return "network_error"
		}
		return "execution_error"
	}
}
