package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/keymanagement"
)

var (
	ErrNoVault        = errors.New("no active vault matched the configured name")
	ErrMultipleVaults = errors.New("multiple active vaults matched the configured name")
)

type Vault struct{ ID, DisplayName, LifecycleState string }

type VaultLister interface {
	List(ctx context.Context, compartmentID, page string) (items []Vault, nextPage, requestID string, err error)
}

type OCIAPI interface {
	ListVaults(ctx context.Context, request keymanagement.ListVaultsRequest) (keymanagement.ListVaultsResponse, error)
}
type OCIListVaults struct{ Client OCIAPI }

func (o OCIListVaults) List(ctx context.Context, compartmentID, page string) ([]Vault, string, string, error) {
	response, err := o.Client.ListVaults(ctx, keymanagement.ListVaultsRequest{
		CompartmentId: common.String(compartmentID), Page: optionalString(page),
		RequestMetadata: common.RequestMetadata{RetryPolicy: retryPolicyPointer()},
	})
	if err != nil {
		var serviceErr common.ServiceError
		if errors.As(err, &serviceErr) {
			return nil, "", serviceErr.GetOpcRequestID(), err
		}
		return nil, "", "", err
	}
	requestID := value(response.OpcRequestId)
	items := make([]Vault, 0, len(response.Items))
	for _, item := range response.Items {
		items = append(items, Vault{ID: value(item.Id), DisplayName: value(item.DisplayName), LifecycleState: string(item.LifecycleState)})
	}
	return items, value(response.OpcNextPage), requestID, nil
}

func ResolveVault(ctx context.Context, lister VaultLister, compartmentID, name string) (Vault, error) {
	seenPages := map[string]struct{}{}
	matches := map[string]Vault{}
	page := ""
	for {
		items, next, requestID, err := listWithRetry(ctx, lister, compartmentID, page)
		if err != nil {
			return Vault{}, fmt.Errorf("list vaults (request_id=%s): %w", requestID, err)
		}
		slog.Info("vault page scanned", "request_id", requestID, "vault_count", len(items))
		for _, item := range items {
			if item.DisplayName == name && item.LifecycleState == "ACTIVE" && item.ID != "" {
				matches[item.ID] = item
			}
		}
		if next == "" {
			break
		}
		if _, exists := seenPages[next]; exists {
			return Vault{}, fmt.Errorf("OCI pagination repeated a page token")
		}
		seenPages[next] = struct{}{}
		page = next
	}
	if len(matches) == 0 {
		slog.Info("vault resolution completed", "match_count", 0)
		return Vault{}, ErrNoVault
	}
	if len(matches) > 1 {
		slog.Info("vault resolution completed", "match_count", len(matches))
		return Vault{}, fmt.Errorf("%w: %d matches", ErrMultipleVaults, len(matches))
	}
	for _, vault := range matches {
		slog.Info("vault resolution completed", "match_count", 1)
		return vault, nil
	}
	return Vault{}, ErrNoVault
}

func listWithRetry(ctx context.Context, lister VaultLister, compartmentID, page string) ([]Vault, string, string, error) {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		var items []Vault
		var next, requestID string
		items, next, requestID, err = lister.List(ctx, compartmentID, page)
		if err == nil || !retryable(err) || attempt == 4 {
			return items, next, requestID, err
		}
		delay := time.Duration(1<<attempt)*200*time.Millisecond + time.Duration(rand.Int63n(int64(150*time.Millisecond)))
		if delay > 10*time.Second {
			delay = 10 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, "", requestID, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, "", "", err
}

func retryable(err error) bool {
	var serviceErr common.ServiceError
	if errors.As(err, &serviceErr) {
		status := serviceErr.GetHTTPStatusCode()
		return status == http.StatusTooManyRequests || status >= 500
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	if err == nil {
		return false
	}
	if strings.Contains(strings.ToLower(err.Error()), "temporary failure") || strings.Contains(strings.ToLower(err.Error()), "connection reset") {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return false
}

func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return common.String(v)
}
func value(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func retryPolicyPointer() *common.RetryPolicy { p := common.NoRetryPolicy(); return &p }

func asDiscovered(v Vault, lifecycle string) Vault { v.LifecycleState = lifecycle; return v }
