package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/secrets"
)

var ErrSecretContent = errors.New("OCI secret bundle has no base64 content")

type SecretBundleAPI interface {
	GetSecretBundleByName(ctx context.Context, request secrets.GetSecretBundleByNameRequest) (secrets.GetSecretBundleByNameResponse, error)
}

// FetchSecretContent reads the current version of the named secret in the
// Vault and returns its decoded content as is. The content is never parsed
// or logged, and does not appear in returned errors.
func FetchSecretContent(ctx context.Context, api SecretBundleAPI, vaultID, secretName string) ([]byte, error) {
	var response secrets.GetSecretBundleByNameResponse
	err := withRetry(ctx, func() error {
		var err error
		response, err = api.GetSecretBundleByName(ctx, secrets.GetSecretBundleByNameRequest{
			SecretName: common.String(secretName), VaultId: common.String(vaultID),
			Stage:           secrets.GetSecretBundleByNameStageCurrent,
			RequestMetadata: common.RequestMetadata{RetryPolicy: retryPolicyPointer()},
		})
		return err
	})
	if err != nil {
		var requestID string
		var serviceErr common.ServiceError
		if errors.As(err, &serviceErr) {
			requestID = serviceErr.GetOpcRequestID()
		}
		return nil, fmt.Errorf("get secret bundle (request_id=%s): %w", requestID, err)
	}
	var encoded *string
	switch content := response.SecretBundleContent.(type) {
	case secrets.Base64SecretBundleContentDetails:
		encoded = content.Content
	case *secrets.Base64SecretBundleContentDetails:
		if content != nil {
			encoded = content.Content
		}
	}
	if encoded == nil {
		return nil, ErrSecretContent
	}
	decoded, err := base64.StdEncoding.DecodeString(*encoded)
	if err != nil {
		return nil, fmt.Errorf("%w: content is not valid base64", ErrSecretContent)
	}
	return decoded, nil
}
