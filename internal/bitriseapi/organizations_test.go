//go:build unit

package bitriseapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListWorkspaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "token bitpat_x" {
			t.Errorf("Authorization = %q, want \"token bitpat_x\"", got)
		}
		if r.URL.Path != "/organizations" {
			t.Errorf("path = %q, want /organizations", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"data":[{"slug":"acme","name":"Acme Inc"},{"slug":"beta","name":"Beta"}]}`)
	}))
	defer srv.Close()

	got, err := ListWorkspaces(context.Background(), srv.URL, "bitpat_x")
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(got) != 2 || got[0].Slug != "acme" || got[0].Name != "Acme Inc" || got[1].Slug != "beta" {
		t.Fatalf("unexpected workspaces: %+v", got)
	}
}

func TestListWorkspaces_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"unauthorized"}`)
	}))
	defer srv.Close()

	if _, err := ListWorkspaces(context.Background(), srv.URL, "bad"); err == nil {
		t.Fatal("expected error on 401")
	}
}

func TestResolveAPIBaseURL(t *testing.T) {
	if got := ResolveAPIBaseURL(nil); got != DefaultAPIBaseURL {
		t.Fatalf("default = %q, want %q", got, DefaultAPIBaseURL)
	}
	if got := ResolveAPIBaseURL(map[string]string{"BITRISE_API_BASE_URL": "https://staging.example/v0.1"}); got != "https://staging.example/v0.1" {
		t.Fatalf("override = %q", got)
	}
}
