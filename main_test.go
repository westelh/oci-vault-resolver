package main

import (
	"context"
	"fmt"
	"net/url"
	"testing"
)

type testServiceError struct{ status int }

func (e testServiceError) Error() string           { return "sensitive response body" }
func (e testServiceError) GetHTTPStatusCode() int  { return e.status }
func (e testServiceError) GetMessage() string      { return "sensitive response body" }
func (e testServiceError) GetCode() string         { return "InternalError" }
func (e testServiceError) GetOpcRequestID() string { return "request-id" }

func TestClassifyOCIServiceErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{status: 401, want: "authentication_error"},
		{status: 403, want: "authorization_error"},
		{status: 429, want: "rate_limited"},
		{status: 503, want: "service_error"},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			err := fmt.Errorf("list vaults: %w", testServiceError{status: tc.status})
			if got := classify(err); got != tc.want {
				t.Fatalf("classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyDoesNotExposeServiceResponseBody(t *testing.T) {
	got := classify(testServiceError{status: 500})
	if got != "service_error" {
		t.Fatalf("classify() = %q", got)
	}
}

func TestClassifyNetworkErrorsWithoutDetails(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"url", &url.Error{Op: "Get", URL: "https://user:secret@example.invalid/path?token=secret", Err: fmt.Errorf("connection reset")}, "network_error"},
		{"net", fakeNetError{}, "network_error"},
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"canceled", context.Canceled, "canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.err); got != tc.want {
				t.Fatalf("classify()=%q want %q", got, tc.want)
			}
		})
	}
}

type fakeNetError struct{}

func (fakeNetError) Error() string   { return "credentials: secret" }
func (fakeNetError) Timeout() bool   { return false }
func (fakeNetError) Temporary() bool { return false }
