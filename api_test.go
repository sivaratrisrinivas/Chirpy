package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Integration tests run against a real Postgres when TEST_DB_URL is set
// (CI provides one as a service container). Without it they are skipped.

var testDB *sql.DB

func TestMain(m *testing.M) {
	url := os.Getenv("TEST_DB_URL")
	if url == "" {
		fmt.Println("TEST_DB_URL not set; skipping integration tests")
		os.Exit(m.Run())
	}
	var err error
	testDB, err = sql.Open("postgres", url)
	if err != nil {
		panic(err)
	}
	if _, err := testDB.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		panic(err)
	}
	if err := migrate(testDB); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

const testPolkaKey = "test-polka-key"

func newTestServer(t *testing.T) (*apiConfig, http.Handler) {
	t.Helper()
	if testDB == nil {
		t.Skip("TEST_DB_URL not set")
	}
	if _, err := testDB.Exec(`TRUNCATE users CASCADE`); err != nil {
		t.Fatal(err)
	}
	api := newAPI(testDB, config{
		Platform:  "dev",
		JWTSecret: strings.Repeat("s", 32),
		PolkaKey:  testPolkaKey,
	})
	api.authLimiter = newIPRateLimiter(1000, time.Minute)
	return api, api.routes()
}

type resp struct {
	code   int
	body   []byte
	header http.Header
}

func do(t *testing.T, h http.Handler, method, path, auth string, body any) resp {
	t.Helper()
	var buf bytes.Buffer
	switch b := body.(type) {
	case nil:
	case string:
		buf.WriteString(b)
	default:
		_ = json.NewEncoder(&buf).Encode(b)
	}
	req := httptest.NewRequest(method, path, &buf)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return resp{rec.Code, rec.Body.Bytes(), rec.Header()}
}

type loginResp struct {
	ID           uuid.UUID `json:"id"`
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token"`
	IsChirpyRed  bool      `json:"is_chirpy_red"`
}

func signup(t *testing.T, h http.Handler, email string) loginResp {
	t.Helper()
	creds := map[string]string{"email": email, "password": "correct-horse"}
	if r := do(t, h, "POST", "/api/users", "", creds); r.code != http.StatusCreated {
		t.Fatalf("signup: %d %s", r.code, r.body)
	}
	r := do(t, h, "POST", "/api/login", "", creds)
	if r.code != http.StatusOK {
		t.Fatalf("login: %d %s", r.code, r.body)
	}
	var lr loginResp
	_ = json.Unmarshal(r.body, &lr)
	return lr
}

func TestSignupValidation(t *testing.T) {
	_, h := newTestServer(t)
	cases := []struct {
		name string
		body any
		want int
	}{
		{"malformed json", "{not json", http.StatusBadRequest},
		{"bad email", map[string]string{"email": "nope", "password": "longenough"}, http.StatusBadRequest},
		{"short password", map[string]string{"email": "a@b.co", "password": "short"}, http.StatusBadRequest},
		{"ok", map[string]string{"email": "a@b.co", "password": "longenough"}, http.StatusCreated},
		{"duplicate", map[string]string{"email": "A@b.co", "password": "longenough"}, http.StatusConflict},
	}
	for _, c := range cases {
		if r := do(t, h, "POST", "/api/users", "", c.body); r.code != c.want {
			t.Errorf("%s: got %d want %d (%s)", c.name, r.code, c.want, r.body)
		}
	}
	big := `{"email":"x@y.co","password":"` + strings.Repeat("a", maxBodyBytes) + `"}`
	if r := do(t, h, "POST", "/api/users", "", big); r.code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: got %d", r.code)
	}
}

func TestLoginRefreshRevoke(t *testing.T) {
	_, h := newTestServer(t)
	u := signup(t, h, "flow@example.com")

	if r := do(t, h, "POST", "/api/login", "", map[string]string{"email": "flow@example.com", "password": "wrong-pass"}); r.code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", r.code)
	}
	if r := do(t, h, "POST", "/api/refresh", "Bearer "+u.RefreshToken, nil); r.code != http.StatusOK {
		t.Fatalf("refresh: %d %s", r.code, r.body)
	}
	// The DB never holds the raw refresh token.
	var n int
	_ = testDB.QueryRow(`SELECT count(*) FROM refresh_tokens WHERE token = $1`, u.RefreshToken).Scan(&n)
	if n != 0 {
		t.Fatal("raw refresh token stored in database")
	}
	if r := do(t, h, "POST", "/api/revoke", "Bearer "+u.RefreshToken, nil); r.code != http.StatusNoContent {
		t.Fatalf("revoke: %d", r.code)
	}
	if r := do(t, h, "POST", "/api/refresh", "Bearer "+u.RefreshToken, nil); r.code != http.StatusUnauthorized {
		t.Fatalf("refresh after revoke: %d", r.code)
	}
}

func TestChirpLifecycleAndOwnership(t *testing.T) {
	_, h := newTestServer(t)
	alice := signup(t, h, "alice@example.com")
	bob := signup(t, h, "bob@example.com")

	if r := do(t, h, "POST", "/api/chirps", "", map[string]string{"body": "hi"}); r.code != http.StatusUnauthorized {
		t.Fatalf("no auth: %d", r.code)
	}
	if r := do(t, h, "POST", "/api/chirps", "Bearer "+alice.Token, map[string]string{"body": strings.Repeat("é", 141)}); r.code != http.StatusBadRequest {
		t.Fatalf("too long: %d", r.code)
	}
	// 140 multi-byte characters is allowed (length is counted in characters).
	if r := do(t, h, "POST", "/api/chirps", "Bearer "+alice.Token, map[string]string{"body": strings.Repeat("é", 140)}); r.code != http.StatusCreated {
		t.Fatalf("140 runes: %d %s", r.code, r.body)
	}
	r := do(t, h, "POST", "/api/chirps", "Bearer "+alice.Token, map[string]string{"body": "what a Kerfuffle today"})
	var c Chirp
	_ = json.Unmarshal(r.body, &c)
	if r.code != http.StatusCreated || c.Body != "what a **** today" {
		t.Fatalf("create: %d %q", r.code, c.Body)
	}
	if r := do(t, h, "DELETE", "/api/chirps/"+c.ID.String(), "Bearer "+bob.Token, nil); r.code != http.StatusForbidden {
		t.Fatalf("bob deletes alice chirp: %d", r.code)
	}
	if r := do(t, h, "DELETE", "/api/chirps/"+c.ID.String(), "Bearer "+alice.Token, nil); r.code != http.StatusNoContent {
		t.Fatalf("owner delete: %d", r.code)
	}
	if r := do(t, h, "GET", "/api/chirps/"+c.ID.String(), "", nil); r.code != http.StatusNotFound {
		t.Fatalf("get deleted: %d", r.code)
	}
	if r := do(t, h, "DELETE", "/api/chirps/"+uuid.NewString(), "Bearer "+alice.Token, nil); r.code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", r.code)
	}
}

func TestListPaginationAndAuthorFilter(t *testing.T) {
	_, h := newTestServer(t)
	alice := signup(t, h, "pa@example.com")
	bob := signup(t, h, "pb@example.com")
	for i := 0; i < 7; i++ {
		do(t, h, "POST", "/api/chirps", "Bearer "+alice.Token, map[string]string{"body": fmt.Sprintf("a%d", i)})
		do(t, h, "POST", "/api/chirps", "Bearer "+bob.Token, map[string]string{"body": fmt.Sprintf("b%d", i)})
	}

	var seen []string
	cursor := ""
	pages := 0
	for {
		path := "/api/chirps?sort=desc&limit=3&author_id=" + alice.ID.String()
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		r := do(t, h, "GET", path, "", nil)
		if r.code != http.StatusOK {
			t.Fatalf("list: %d %s", r.code, r.body)
		}
		var page []Chirp
		_ = json.Unmarshal(r.body, &page)
		for _, c := range page {
			if c.UserID != alice.ID {
				t.Fatalf("author filter leaked %v", c.UserID)
			}
			seen = append(seen, c.Body)
		}
		pages++
		cursor = r.header.Get("X-Next-Cursor")
		if cursor == "" {
			break
		}
	}
	want := "a6,a5,a4,a3,a2,a1,a0"
	if got := strings.Join(seen, ","); got != want || pages != 3 {
		t.Fatalf("got %s in %d pages, want %s in 3", got, pages, want)
	}

	for _, bad := range []string{"?sort=up", "?limit=0", "?limit=101", "?author_id=x", "?cursor=!!"} {
		if r := do(t, h, "GET", "/api/chirps"+bad, "", nil); r.code != http.StatusBadRequest {
			t.Errorf("%s: got %d", bad, r.code)
		}
	}
}

func TestPolkaWebhook(t *testing.T) {
	_, h := newTestServer(t)
	u := signup(t, h, "red@example.com")
	ev := map[string]any{"event": "user.upgraded", "data": map[string]string{"user_id": u.ID.String()}}

	if r := do(t, h, "POST", "/api/polka/webhooks", "", ev); r.code != http.StatusUnauthorized {
		t.Fatalf("no key: %d", r.code)
	}
	if r := do(t, h, "POST", "/api/polka/webhooks", "ApiKey wrong", ev); r.code != http.StatusUnauthorized {
		t.Fatalf("wrong key: %d", r.code)
	}
	for i := 0; i < 2; i++ { // retried delivery is idempotent
		if r := do(t, h, "POST", "/api/polka/webhooks", "ApiKey "+testPolkaKey, ev); r.code != http.StatusNoContent {
			t.Fatalf("upgrade attempt %d: %d", i, r.code)
		}
	}
	var red bool
	_ = testDB.QueryRow(`SELECT is_chirpy_red FROM users WHERE id=$1`, u.ID).Scan(&red)
	if !red {
		t.Fatal("user not upgraded")
	}
	unknown := map[string]any{"event": "user.upgraded", "data": map[string]string{"user_id": uuid.NewString()}}
	if r := do(t, h, "POST", "/api/polka/webhooks", "ApiKey "+testPolkaKey, unknown); r.code != http.StatusNotFound {
		t.Fatalf("unknown user: %d", r.code)
	}
	other := map[string]any{"event": "user.payment_failed", "data": map[string]string{"user_id": u.ID.String()}}
	if r := do(t, h, "POST", "/api/polka/webhooks", "ApiKey "+testPolkaKey, other); r.code != http.StatusNoContent {
		t.Fatalf("ignored event: %d", r.code)
	}
}

func TestStaticDoesNotExposeRepo(t *testing.T) {
	_, h := newTestServer(t)
	for _, p := range []string{"/app/../.env", "/app/main.go", "/app/go.mod"} {
		if r := do(t, h, "GET", p, "", nil); r.code == http.StatusOK {
			t.Errorf("%s served with 200", p)
		}
	}
	if r := do(t, h, "GET", "/app/", "", nil); r.code != http.StatusOK {
		t.Errorf("/app/ not served: %d", r.code)
	}
	if r := do(t, h, "GET", "/api/healthz", "", nil); r.code != http.StatusOK {
		t.Errorf("healthz: %d", r.code)
	}
}

func TestRateLimiter(t *testing.T) {
	l := newIPRateLimiter(2, time.Minute)
	now := time.Now()
	if ok, _ := l.allow("1.2.3.4", now); !ok {
		t.Fatal("1st")
	}
	if ok, _ := l.allow("1.2.3.4", now); !ok {
		t.Fatal("2nd")
	}
	if ok, _ := l.allow("1.2.3.4", now); ok {
		t.Fatal("3rd should be limited")
	}
	if ok, _ := l.allow("5.6.7.8", now); !ok {
		t.Fatal("other ip limited")
	}
	if ok, _ := l.allow("1.2.3.4", now.Add(time.Minute)); !ok {
		t.Fatal("window reset")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	id := uuid.New()
	ts := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
	cur := encodeCursor(databaseChirp(id, ts))
	gt, gid, err := decodeCursor(cur)
	if err != nil || !gt.Equal(ts) || gid != id {
		t.Fatalf("round trip: %v %v %v", gt, gid, err)
	}
}
