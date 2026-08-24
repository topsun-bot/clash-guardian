package clash

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientReadsAndSwitches(t *testing.T) {
	selected := "bad"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/version":
			_ = json.NewEncoder(w).Encode(Version{Meta: true, Version: "v-test"})
		case r.URL.Path == "/configs":
			_ = json.NewEncoder(w).Encode(RuntimeConfig{Mode: "rule", MixedPort: 7890})
		case r.URL.Path == "/proxies" && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(ProxiesResponse{Proxies: map[string]Proxy{
				"Proxy": {Name: "Proxy", Type: "Selector", Now: selected, All: []string{"bad", "good"}},
			}})
		case r.URL.Path == "/proxies/Proxy" && r.Method == http.MethodPut:
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			selected = body["name"]
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/proxies/good/delay":
			_ = json.NewEncoder(w).Encode(map[string]int{"delay": 42})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", "secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	version, err := client.Version(ctx)
	if err != nil || version.Version != "v-test" {
		t.Fatalf("Version() = %#v, %v", version, err)
	}
	runtimeConfig, err := client.RuntimeConfig(ctx)
	if err != nil || runtimeConfig.Mode != "rule" {
		t.Fatalf("RuntimeConfig() = %#v, %v", runtimeConfig, err)
	}
	if err := client.Select(ctx, "Proxy", "good"); err != nil {
		t.Fatal(err)
	}
	if selected != "good" {
		t.Fatalf("未切换，selected=%q", selected)
	}
	delay, err := client.Delay(ctx, "good", "https://example.com", time.Second)
	if err != nil || delay != 42 {
		t.Fatalf("Delay() = %d, %v", delay, err)
	}
}
