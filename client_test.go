package runcloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSandboxLifecycleUsesPublicRoutes(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer rc_live_test" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/run-cloud/sandboxes" && r.Method == http.MethodPost:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["image"] != "runcloud/agent-base" || body["cpu"] != 1.5 {
				t.Fatalf("create body = %#v", body)
			}
			io.WriteString(w, `{"id":"sb_go","name":"go","state":"running"}`)
		case r.URL.Path == "/run-cloud/sandboxes/sb_go/exec":
			io.WriteString(w, `{"exit_code":0,"stdout":"ok\n","stderr":""}`)
		case r.URL.Path == "/run-cloud/sandboxes/sb_go/fs":
			io.WriteString(w, `{"content":"`+base64.StdEncoding.EncodeToString([]byte("artifact"))+`"}`)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			io.WriteString(w, `{"id":"sb_go","name":"go","state":"running"}`)
		}
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{APIKey: "rc_live_test", APIURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	box, err := client.CreateSandbox(context.Background(), CreateSandboxOptions{CPU: 1.5})
	if err != nil {
		t.Fatal(err)
	}
	result, err := box.Exec(context.Background(), "echo ok", ExecOptions{})
	if err != nil || result.Stdout != "ok\n" {
		t.Fatalf("Exec = %#v, %v", result, err)
	}
	artifact, err := box.ReadFile(context.Background(), "/tmp/a")
	if err != nil || string(artifact) != "artifact" {
		t.Fatalf("ReadFile = %q, %v", artifact, err)
	}
	if err := box.Pause(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := box.Resume(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := box.Destroy(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"POST /run-cloud/sandboxes",
		"POST /run-cloud/sandboxes/sb_go/exec",
		"POST /run-cloud/sandboxes/sb_go/fs",
		"POST /run-cloud/sandboxes/sb_go/pause",
		"POST /run-cloud/sandboxes/sb_go/resume",
		"DELETE /run-cloud/sandboxes/sb_go",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls[%d] = %q, want %q", i, calls[i], want[i])
		}
	}
}
