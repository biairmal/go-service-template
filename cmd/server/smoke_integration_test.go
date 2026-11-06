package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/biairmal/go-service-template/internal/module/note"
	"github.com/biairmal/go-service-template/internal/platform/testkit"
)

// smokeRoles replaces the empty authz role map in config/config.yaml: the
// test's member role may read and write its own notes.
const smokeRoles = `roles: { member: ["notes:read", "notes:write"] }`

// TestSmoke builds the real binary and drives it as an operator would:
// migrate up, serve with auth on, a note round trip with a signed token,
// then SIGTERM.
func TestSmoke(t *testing.T) {
	db := testkit.PostgresConfig(t)
	keys := testkit.SigningKeys(t, "identity", "api")
	dir := t.TempDir()
	bin := buildServer(t, dir)
	writeConfig(t, dir)
	addr := freeAddr(t)

	env := append(os.Environ(),
		"APP_ENV=local",
		"HTTP_ADDR="+addr,
		"DB_HOST="+db.Leader.Host,
		"DB_PORT="+strconv.Itoa(db.Leader.Port),
		"DB_NAME="+db.Leader.Database,
		"DB_USER="+db.Leader.Username,
		"DB_PASSWORD="+db.Leader.Password,
		"DB_SSLMODE=disable",
		"AUTH_ENABLED=true",
		"AUTH_JWKS_URL="+keys.JWKSURL,
		"AUTH_ISSUER=identity",
		"AUTH_AUDIENCE=api",
		"AUTHZ_SOURCE=config",
		"TRACING_ENABLED=false",
		"SWAGGER_ENABLED=false",
		"DRAIN_DELAY=0s",
	)

	migrate := exec.CommandContext(t.Context(), bin, "migrate", "up")
	migrate.Dir, migrate.Env = dir, env
	if out, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migrate up: %v\n%s", err, out)
	}

	var logs syncBuffer
	serve := exec.Command(bin, "serve")
	serve.Dir, serve.Env = dir, env
	serve.Stdout, serve.Stderr = &logs, &logs
	if err := serve.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- serve.Wait() }()
	t.Cleanup(func() {
		_ = serve.Process.Kill()
		if t.Failed() {
			t.Logf("server output:\n%s", logs.String())
		}
	})

	// The binary is not an httptest.Server; a proxy in front of it lets the
	// test kit's Do helper drive it.
	srv := testkit.Server(t, httputil.NewSingleHostReverseProxy(&url.URL{Scheme: "http", Host: addr}))
	waitReady(t, "http://"+addr, exited)

	if r := testkit.Do[any](t, srv, http.MethodGet, "/healthz", nil, nil); r.Status != http.StatusOK {
		t.Fatalf("/healthz = %d", r.Status)
	}
	if r := testkit.Do[any](t, srv, http.MethodGet, "/v1/notes", nil, nil); r.Status != http.StatusUnauthorized {
		t.Fatalf("GET /v1/notes without a token = %d, want 401", r.Status)
	}

	auth := testkit.Bearer(keys.Token(t, uuid.NewString(), uuid.NewString(), []string{"member"}, nil))
	created := testkit.Do[note.NoteResponse](t, srv, http.MethodPost, "/v1/notes",
		note.CreateNoteRequest{Title: "smoke", Body: "from the real binary"}, auth)
	if created.Status != http.StatusCreated || created.Data.ID == "" {
		t.Fatalf("create = %d: %s", created.Status, created.Raw)
	}
	list := testkit.Do[note.NotePageResponse](t, srv, http.MethodGet, "/v1/notes", nil, auth)
	if list.Status != http.StatusOK || list.Data.Total != 1 || list.Data.Items[0].ID != created.Data.ID {
		t.Fatalf("list = %d: %s", list.Status, list.Raw)
	}
	got := testkit.Do[note.NoteResponse](t, srv, http.MethodGet, "/v1/notes/"+created.Data.ID, nil, auth)
	if got.Status != http.StatusOK || got.Data.Title != "smoke" {
		t.Fatalf("get = %d: %s", got.Status, got.Raw)
	}

	if runtime.GOOS == "windows" {
		t.Log("SIGTERM cannot be sent on Windows; graceful exit is checked on Linux (CI)")
		return
	}
	if err := serve.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("serve exited with %v, want 0", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not exit within 30s of SIGTERM")
	}
}

// buildServer compiles this package into dir.
func buildServer(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "server")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// writeConfig copies the repo's config/config.yaml into dir with the smoke
// test's roles, since a role map can't be set from the environment.
func writeConfig(t *testing.T, dir string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", configFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("roles: {}")) {
		t.Fatal(`config/config.yaml no longer has "roles: {}"; update smokeRoles`)
	}
	b = bytes.Replace(b, []byte("roles: {}"), []byte(smokeRoles), 1)
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, configFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// freeAddr returns a loopback address with a port nothing listens on.
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// waitReady polls /readyz until it answers 200, failing early if the
// process exits first.
func waitReady(t *testing.T, base string, exited <-chan error) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			t.Fatalf("serve exited before it was ready: %v", err)
		default:
		}
		if readyz(t.Context(), base) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("serve not ready within 30s")
}

func readyz(ctx context.Context, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/readyz", http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("readyz = %d", resp.StatusCode)
	}
	return nil
}

// syncBuffer collects the server's output from two pipes at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
