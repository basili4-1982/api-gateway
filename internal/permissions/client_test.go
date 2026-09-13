package permissions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_GetEffectivePermissions_UsesConfiguredEndpoint(t *testing.T) {
	var gotMethod, gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(UserPermissions{UserID: 42, Permissions: []string{"read"}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "secret", "X-API-Key", http.MethodGet, "/api/v1/users/{user_id}/effective-permissions")
	perms, err := c.GetEffectivePermissions(42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/api/v1/users/42/effective-permissions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotKey != "secret" {
		t.Errorf("X-API-Key = %q, want secret", gotKey)
	}
	if len(perms.Permissions) != 1 || perms.Permissions[0] != "read" {
		t.Errorf("permissions = %v", perms.Permissions)
	}
}

func TestClient_GetEffectivePermissions_CustomEndpoint(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewEncoder(w).Encode(UserPermissions{UserID: 7, Permissions: []string{"write"}}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "token-123", "Authorization", http.MethodPost, "/v2/users/{user_id}/perms")
	if _, err := c.GetEffectivePermissions(7); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v2/users/7/perms" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "token-123" {
		t.Errorf("Authorization = %q, want token-123", gotAuth)
	}
}

func TestClient_GetEffectivePermissions_NoAPIKeyOmitsHeader(t *testing.T) {
	var hasAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasAuth = r.Header["Authorization"]
		if err := json.NewEncoder(w).Encode(UserPermissions{UserID: 1}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "", "Authorization", http.MethodGet, "/v2/users/{user_id}/perms")
	if _, err := c.GetEffectivePermissions(1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasAuth {
		t.Error("Authorization header must be omitted when api_key is empty")
	}
}
