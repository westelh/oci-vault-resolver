package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/keymanagement"
)

type mockOCIAPI struct {
	requests  []keymanagement.ListVaultsRequest
	responses []keymanagement.ListVaultsResponse
	err       error
}

func (m *mockOCIAPI) ListVaults(_ context.Context, request keymanagement.ListVaultsRequest) (keymanagement.ListVaultsResponse, error) {
	m.requests = append(m.requests, request)
	r := m.responses[0]
	m.responses = m.responses[1:]
	return r, m.err
}

func TestOCIAdapterMapsPaginationAndDoesNotFetchSecrets(t *testing.T) {
	next := "page-2"
	id := "v1"
	name := "vault"
	state := keymanagement.VaultSummaryLifecycleStateActive
	api := &mockOCIAPI{responses: []keymanagement.ListVaultsResponse{
		{Items: []keymanagement.VaultSummary{{Id: &id, DisplayName: &name, LifecycleState: state}}, OpcNextPage: &next, OpcRequestId: common.String("req-1"), RawResponse: &http.Response{Header: http.Header{"Opc-Request-Id": []string{"req-1"}}}},
		{Items: []keymanagement.VaultSummary{{Id: &id, DisplayName: &name, LifecycleState: state}}},
	}}
	lister := OCIListVaults{Client: api}
	vault, err := ResolveVault(context.Background(), lister, "compartment", "vault")
	if err != nil {
		t.Fatal(err)
	}
	if vault.ID != "v1" || len(api.requests) != 2 {
		t.Fatalf("vault=%+v requests=%d", vault, len(api.requests))
	}
	if api.requests[0].Page != nil || api.requests[1].Page == nil || *api.requests[1].Page != "page-2" {
		t.Fatalf("requests=%+v", api.requests)
	}
	if api.requests[0].CompartmentId == nil || *api.requests[0].CompartmentId != "compartment" {
		t.Fatalf("request=%+v", api.requests[0])
	}
	if api.requests[0].RequestMetadata.RetryPolicy == nil {
		t.Fatal("OCI SDK retry policy must be disabled to avoid nested retries")
	}
}
