package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type pageResult struct {
	items           []Vault
	next, requestID string
	err             error
}
type mockVaultLister struct {
	calls   []string
	results map[string][]pageResult
}

func (m *mockVaultLister) List(_ context.Context, compartment, page string) ([]Vault, string, string, error) {
	m.calls = append(m.calls, compartment+"/"+page)
	results := m.results[page]
	if len(results) == 0 {
		return nil, "", "", fmt.Errorf("unexpected page %q", page)
	}
	r := results[0]
	if len(results) > 1 {
		m.results[page] = results[1:]
	}
	return r.items, r.next, r.requestID, r.err
}

func TestResolveVaultScansPagesAndRequiresActiveExactMatch(t *testing.T) {
	l := &mockVaultLister{results: map[string][]pageResult{
		"":     {{items: []Vault{{ID: "inactive", DisplayName: "wanted", LifecycleState: "DELETING"}, {ID: "other", DisplayName: "wanted-suffix", LifecycleState: "ACTIVE"}}, next: "next", requestID: "r1"}},
		"next": {{items: []Vault{{ID: "v1", DisplayName: "wanted", LifecycleState: "ACTIVE"}, {ID: "v1", DisplayName: "wanted", LifecycleState: "ACTIVE"}}, requestID: "r2"}},
	}}
	v, err := ResolveVault(context.Background(), l, "compartment", "wanted")
	if err != nil {
		t.Fatal(err)
	}
	if v.ID != "v1" || len(l.calls) != 2 {
		t.Fatalf("vault=%+v calls=%v", v, l.calls)
	}
}

func TestResolveVaultRequiresExactlyOne(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []Vault
		want  error
	}{
		{"none", nil, ErrNoVault},
		{"multiple", []Vault{{ID: "v1", DisplayName: "wanted", LifecycleState: "ACTIVE"}, {ID: "v2", DisplayName: "wanted", LifecycleState: "ACTIVE"}}, ErrMultipleVaults},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &mockVaultLister{results: map[string][]pageResult{"": {{items: tc.items}}}}
			_, err := ResolveVault(context.Background(), l, "c", "wanted")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestResolveVaultStopsOnPageFailureWithoutFallback(t *testing.T) {
	l := &mockVaultLister{results: map[string][]pageResult{
		"":     {{items: []Vault{{ID: "old", DisplayName: "wanted", LifecycleState: "ACTIVE"}}, next: "next"}},
		"next": {{err: errors.New("permission denied"), requestID: "request"}},
	}}
	_, err := ResolveVault(context.Background(), l, "c", "wanted")
	if err == nil || len(l.calls) != 2 {
		t.Fatalf("err=%v calls=%v", err, l.calls)
	}
}

func TestListRetriesAtMostFiveTimes(t *testing.T) {
	transient := errors.New("temporary failure")
	l := &mockVaultLister{results: map[string][]pageResult{"": {
		{err: transient}, {err: transient}, {err: transient}, {err: transient}, {err: transient}, {items: []Vault{{ID: "v", DisplayName: "wanted", LifecycleState: "ACTIVE"}}},
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := ResolveVault(ctx, l, "c", "wanted")
	if err == nil || len(l.calls) != 5 {
		t.Fatalf("err=%v calls=%d", err, len(l.calls))
	}
}

func TestResolveVaultDetectsRepeatedPage(t *testing.T) {
	l := &mockVaultLister{results: map[string][]pageResult{"": {{next: "next"}}, "next": {{next: "next"}}}}
	_, err := ResolveVault(context.Background(), l, "c", "wanted")
	if err == nil {
		t.Fatal("expected pagination cycle error")
	}
}
