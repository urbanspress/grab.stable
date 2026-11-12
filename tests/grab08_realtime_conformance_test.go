package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"grab/internal/contracts"
	"grab/internal/interpreter"
	"grab/internal/parser"
	"grab/internal/platform"
	"grab/internal/server"
)

func realtimeOptions(t *testing.T) interpreter.Options {
	t.Helper()
	path, e := filepath.Abs("../examples/realtime.platform.json")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := platform.Parse(raw, path)
	if e != nil {
		t.Fatal(e)
	}
	g, e := cfg.Grant(map[string][]string{"channel": {"updates:connect", "updates:publish"}})
	if e != nil {
		t.Fatal(e)
	}
	api, e := contracts.Load("../examples/realtime.api.json")
	if e != nil {
		t.Fatal(e)
	}
	return interpreter.Options{PlatformConfig: cfg, PlatformGrants: g, APIContract: api}
}
func TestGRAB08AuthorizedRealtimeAndRefetch(t *testing.T) {
	path, _ := filepath.Abs("../examples/realtime_app.grab")
	app, e := server.NewApplication(path, "", realtimeOptions(t), server.DefaultConfig(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	defer app.Close()
	srv := httptest.NewServer(app)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	headers := http.Header{"Origin": {"http://127.0.0.1:8080"}, "Authorization": {"Bearer local-demo"}}
	client, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/live", &websocket.DialOptions{HTTPHeader: headers})
	if e != nil {
		t.Fatal(e)
	}
	defer client.CloseNow()
	if e = client.Write(ctx, websocket.MessageText, []byte(`{"audience":"demo","value":"refresh"}`)); e != nil {
		t.Fatal(e)
	}
	_, raw, e := client.Read(ctx)
	if e != nil || !strings.Contains(string(raw), `"refresh"`) {
		t.Fatal(string(raw), e)
	}
	request, _ := http.NewRequestWithContext(ctx, "POST", srv.URL+"/notify", nil)
	request.Header.Set("Authorization", "Bearer local-demo")
	resp, e := http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 202 {
		t.Fatal(resp.Status)
	}
	_, raw, e = client.Read(ctx)
	if e != nil || !strings.Contains(string(raw), "Catalog changed") {
		t.Fatal(string(raw), e)
	}
	resp, e = http.Get(srv.URL + "/")
	if e != nil {
		t.Fatal(e)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "authoritative") {
		t.Fatal(resp.Status, string(body))
	}
	// Host grants do not bypass the application's audience authorization.
	request, _ = http.NewRequestWithContext(ctx, "POST", srv.URL+"/notify", nil)
	resp, e = http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatal("unauthorized publication", resp.Status)
	}
	for _, header := range []http.Header{{"Origin": {"http://127.0.0.1:8080"}}, {"Origin": {"https://evil.example"}, "Authorization": {"Bearer local-demo"}}} {
		c, resp, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/live", &websocket.DialOptions{HTTPHeader: header})
		if c != nil {
			c.CloseNow()
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if e == nil {
			t.Fatal("unauthorized upgrade")
		}
	}
}
func TestGRAB08RealtimeStaticRejections(t *testing.T) {
	raw, e := os.ReadFile("../examples/realtime_app.grab")
	if e != nil {
		t.Fatal(e)
	}
	for _, source := range []string{strings.Replace(string(raw), `"updates", message.audience`, `message.audience, message.audience`, 1), string(raw) + "\nroute \"GET\" \"/live\" using request\n give back server.text using 200, \"collision\"\nend\n", strings.Replace(string(raw), "make authorize using request", "make different using request", 1)} {
		p, e := parser.Parse("bad.grab", source)
		if e != nil {
			t.Fatal(e)
		}
		opts := realtimeOptions(t)
		if e = interpreter.ValidateSockets(p, opts.PlatformConfig, opts.APIContract); e == nil {
			t.Fatal("invalid channel program accepted")
		}
	}
}

func TestGRAB08ChannelOnlyApplicationAndIndependentAdmissions(t *testing.T) {
	raw, e := os.ReadFile("../examples/realtime_app.grab")
	if e != nil {
		t.Fatal(e)
	}
	// A channel-only application must not require a legacy HTML template.
	source := strings.Split(string(raw), "route ")[0]
	path := filepath.Join(t.TempDir(), "channels.grab")
	if e = os.WriteFile(path, []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	cfg := server.DefaultConfig()
	cfg.MaxConcurrency = 1
	app, e := server.NewApplication(path, "", realtimeOptions(t), cfg, io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	defer app.Close()
	srv := httptest.NewServer(app)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		client, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/live", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://127.0.0.1:8080"}, "Authorization": {"Bearer local-demo"}}})
		if e != nil {
			t.Fatal("established channel retained HTTP admission slot", e)
		}
		defer client.CloseNow()
	}
}

func TestGRAB08ChannelTrustedTLSProxy(t *testing.T) {
	raw, e := os.ReadFile("../examples/realtime.platform.json")
	if e != nil {
		t.Fatal(e)
	}
	raw = []byte(strings.ReplaceAll(strings.ReplaceAll(string(raw), `"development": true`, `"development": false`), "http://127.0.0.1:8080", "https://shop.example"))
	platformConfig, e := platform.Parse(raw, filepath.Join(t.TempDir(), "platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, trusted := range []bool{false, true} {
		opts := realtimeOptions(t)
		opts.PlatformConfig = platformConfig
		opts.PlatformGrants, _ = platformConfig.Grant(map[string][]string{"channel": {"updates:connect", "updates:publish"}})
		cfg := server.DefaultConfig()
		if trusted {
			cfg.TrustedProxy = "127.0.0.1/32"
		}
		path, _ := filepath.Abs("../examples/realtime_app.grab")
		app, e := server.NewApplication(path, "", opts, cfg, io.Discard)
		if e != nil {
			t.Fatal(e)
		}
		srv := httptest.NewServer(app)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		ws, resp, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/live", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://shop.example"}, "Authorization": {"Bearer local-demo"}, "X-Forwarded-Proto": {"https"}}})
		if ws != nil {
			ws.CloseNow()
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		cancel()
		srv.Close()
		app.Close()
		if (e == nil) != trusted {
			t.Fatalf("trusted=%v: %v", trusted, e)
		}
	}
}
