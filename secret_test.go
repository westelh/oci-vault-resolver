package main

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/secrets"
)

type mockSecretAPI struct {
	requests  []secrets.GetSecretBundleByNameRequest
	responses []secrets.GetSecretBundleByNameResponse
	errs      []error
}

func (m *mockSecretAPI) GetSecretBundleByName(_ context.Context, request secrets.GetSecretBundleByNameRequest) (secrets.GetSecretBundleByNameResponse, error) {
	i := len(m.requests)
	m.requests = append(m.requests, request)
	var err error
	if i < len(m.errs) {
		err = m.errs[i]
	}
	if err != nil {
		return secrets.GetSecretBundleByNameResponse{}, err
	}
	return m.responses[0], nil
}

func bundle(content secrets.SecretBundleContentDetails) secrets.GetSecretBundleByNameResponse {
	return secrets.GetSecretBundleByNameResponse{SecretBundle: secrets.SecretBundle{SecretBundleContent: content}}
}

func TestFetchSecretContentReturnsDecodedContentAsIs(t *testing.T) {
	raw := `{"nested":{"value":1}, "spaced" : "kept"}`
	api := &mockSecretAPI{responses: []secrets.GetSecretBundleByNameResponse{
		bundle(secrets.Base64SecretBundleContentDetails{Content: common.String(base64.StdEncoding.EncodeToString([]byte(raw)))}),
	}}
	got, err := FetchSecretContent(context.Background(), api, "vault-1", "config")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != raw {
		t.Fatalf("content=%q", got)
	}
	request := api.requests[0]
	if value(request.VaultId) != "vault-1" || value(request.SecretName) != "config" || request.Stage != secrets.GetSecretBundleByNameStageCurrent {
		t.Fatalf("request=%+v", request)
	}
	if request.RequestMetadata.RetryPolicy == nil {
		t.Fatal("OCI SDK retry policy must be disabled to avoid nested retries")
	}
}

func TestFetchSecretContentRejectsMissingOrInvalidContent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content secrets.SecretBundleContentDetails
	}{
		{"no content", nil},
		{"nil base64", secrets.Base64SecretBundleContentDetails{}},
		{"invalid base64", secrets.Base64SecretBundleContentDetails{Content: common.String("not base64 secret!")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &mockSecretAPI{responses: []secrets.GetSecretBundleByNameResponse{bundle(tc.content)}}
			_, err := FetchSecretContent(context.Background(), api, "vault-1", "config")
			if !errors.Is(err, ErrSecretContent) || classify(err) != "secret_content_invalid" {
				t.Fatalf("err=%v", err)
			}
			if strings.Contains(err.Error(), "secret!") {
				t.Fatalf("error exposes content: %v", err)
			}
		})
	}
}

func TestFetchSecretContentRetriesOnlyTransientErrors(t *testing.T) {
	ok := bundle(secrets.Base64SecretBundleContentDetails{Content: common.String(base64.StdEncoding.EncodeToString([]byte("{}")))})
	api := &mockSecretAPI{responses: []secrets.GetSecretBundleByNameResponse{ok}, errs: []error{testServiceError{status: 503}}}
	if _, err := FetchSecretContent(context.Background(), api, "vault-1", "config"); err != nil || len(api.requests) != 2 {
		t.Fatalf("err=%v requests=%d", err, len(api.requests))
	}
	api = &mockSecretAPI{responses: []secrets.GetSecretBundleByNameResponse{ok}, errs: []error{testServiceError{status: 404}}}
	_, err := FetchSecretContent(context.Background(), api, "vault-1", "config")
	if err == nil || len(api.requests) != 1 || classify(err) != "oci_service_error" {
		t.Fatalf("err=%v requests=%d", err, len(api.requests))
	}
}
