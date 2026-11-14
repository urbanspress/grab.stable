package interpreter

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"

	"grab/internal/lexer"
	"grab/internal/parser"
	grabrt "grab/internal/runtime"
)

func runWithAllEngines(t *testing.T, source string, opts Options) string {
	t.Helper()
	prog, err := parser.Parse("crypto_env_test.grab", source)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	var baseline string
	for i, mode := range []EngineMode{EngineAST, EngineVM, EngineAuto} {
		o := opts
		o.Engine = mode
		var buf bytes.Buffer
		if err := RunWithOptions(prog, &buf, o); err != nil {
			t.Fatalf("engine=%s run failed: %v", mode, err)
		}
		got := buf.String()
		if i == 0 {
			baseline = got
		} else if got != baseline {
			t.Fatalf("engine=%s output mismatch:\ngot:\n%s\nwant:\n%s", mode, got, baseline)
		}
	}
	return baseline
}

func runExpectErrorAllEngines(t *testing.T, source string, opts Options, wantKind ErrorKind, wantCode string) {
	t.Helper()
	prog, err := parser.Parse("crypto_env_err.grab", source)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	for _, mode := range []EngineMode{EngineAST, EngineVM, EngineAuto} {
		o := opts
		o.Engine = mode
		var buf bytes.Buffer
		err := RunWithOptions(prog, &buf, o)
		if err == nil {
			t.Fatalf("engine=%s expected error (%s), succeeded with output %q", mode, wantCode, buf.String())
		}
		var iErr *Error
		if !errors.As(err, &iErr) {
			t.Fatalf("engine=%s expected *interpreter.Error, got %T: %v", mode, err, err)
		}
		if iErr.Kind != wantKind || iErr.Code() != wantCode {
			t.Fatalf("engine=%s error = (%v, %s), want (%v, %s): %v", mode, iErr.Kind, iErr.Code(), wantKind, wantCode, err)
		}
	}
}

func TestCryptoPasswordHashingAndVerify(t *testing.T) {
	src := `use crypto
use text
set pw to "S3cur3-P@ssphrase!"
set h1 to crypto.hash_password using pw
set h2 to crypto.hash_password using pw
show text.starts_with using h1, "$argon2id$v=19$m=19456,t=2,p=1$"
show h1 is not h2
show crypto.verify_password using pw, h1
show crypto.verify_password using pw, h2
show crypto.verify_password using "wrong-password", h1
show crypto.verify_password using "", h1
try
	set bad to crypto.verify_password using pw, "$bcrypt$bad"
catch err
	show err.code
end
try
	set bad2 to crypto.verify_password using pw, "$argon2id$v=19$m=99999999,t=2,p=1$c2FsdHNhbHQ$aGFzaGhhc2hoYXNoaGFzaGhhc2g"
catch err
	show err.code
end`
	out := runWithAllEngines(t, src, Options{})
	want := "yes\nyes\nyes\nyes\nno\nno\nGRAB-E309\nGRAB-E309\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}

	runExpectErrorAllEngines(t, `use crypto
show crypto.hash_password using 123`, Options{}, TypeMismatch, "GRAB-E301")
	runExpectErrorAllEngines(t, `use crypto
show crypto.verify_password using "pw", 123`, Options{}, TypeMismatch, "GRAB-E301")

	tooLong := strings.Repeat("a", maxPasswordBytes+1)
	runExpectErrorAllEngines(t, "use crypto\nshow crypto.hash_password using \""+tooLong+"\"", Options{}, StandardLibraryError, "GRAB-E309")
}

func TestCryptoRandomTokensAndHashes(t *testing.T) {
	// Test NIST SHA-256 ("abc") and RFC 7636 PKCE Appendix B vector across all engines.
	src := `use crypto
use text
set u16 to crypto.random_urlsafe using 16
set u32 to crypto.random_urlsafe using 32
set u64 to crypto.random_urlsafe using 64
set x16 to crypto.random_hex using 16
set x32 to crypto.random_hex using 32
set x64 to crypto.random_hex using 64
show text.length using u16
show text.length using u32
show text.length using u64
show u32 is not crypto.random_urlsafe using 32
show text.length using x16
show text.length using x32
show text.length using x64
show x32 is not crypto.random_hex using 32
show crypto.sha256_hex using "abc"
show crypto.sha256_base64url using "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
show crypto.sha256_hex using b"abc"`
	out := runWithAllEngines(t, src, Options{})
	want := strings.Join([]string{
		"22",
		"43",
		"86",
		"yes",
		"32",
		"64",
		"128",
		"yes",
		"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
	}, "\n") + "\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}

	// Verify charsets directly using stdlib calls.
	prog, err := parser.Parse("charset.grab", `use crypto
set u to crypto.random_urlsafe using 32
set h to crypto.random_hex using 32`)
	if err != nil {
		t.Fatal(err)
	}
	env, err := RunWithEnvironment(prog, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	urlRE := regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	hexRE := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if !urlRE.MatchString(env["u"].TextValue()) {
		t.Fatalf("unexpected urlsafe token format: %q", env["u"].TextValue())
	}
	if !hexRE.MatchString(env["h"].TextValue()) {
		t.Fatalf("unexpected hex token format: %q", env["h"].TextValue())
	}

	// Bounds and type checks.
	for _, bad := range []string{
		`use crypto
show crypto.random_urlsafe using 15`,
		`use crypto
show crypto.random_urlsafe using 65`,
		`use crypto
show crypto.random_hex using 0`,
		`use crypto
show crypto.random_hex using -16`,
		`use crypto
show crypto.random_hex using 100`,
	} {
		runExpectErrorAllEngines(t, bad, Options{}, StandardLibraryError, "GRAB-E309")
	}
	runExpectErrorAllEngines(t, `use crypto
show crypto.random_urlsafe using "32"`, Options{}, TypeMismatch, "GRAB-E301")
	runExpectErrorAllEngines(t, `use crypto
show crypto.sha256_hex using 42`, Options{}, TypeMismatch, "GRAB-E301")
}

func TestCryptoAEADEncryptDecrypt(t *testing.T) {
	rawKey := make([]byte, 32)
	if _, err := rand.Read(rawKey); err != nil {
		t.Fatal(err)
	}
	hexKey := hex.EncodeToString(rawKey)
	b64URLKey := base64.RawURLEncoding.EncodeToString(rawKey)
	stdB64Key := base64.StdEncoding.EncodeToString(rawKey)

	src := `use crypto
use text
set hex_key to "` + hexKey + `"
set url_key to "` + b64URLKey + `"
set std_key to "` + stdB64Key + `"
set ascii_key to "0123456789abcdef0123456789abcdef"
set msg to "oauth-refresh-token-秘密-payload"

set ct1 to crypto.encrypt_aead using msg, hex_key
set ct2 to crypto.encrypt_aead using msg, hex_key
show text.starts_with using ct1, "v1:"
show ct1 is not ct2
show (crypto.decrypt_aead using ct1, hex_key) is msg
show (crypto.decrypt_aead using ct1, url_key) is msg
show (crypto.decrypt_aead using ct2, std_key) is msg

set ct_ascii to crypto.encrypt_aead using "hello", ascii_key
show crypto.decrypt_aead using ct_ascii, ascii_key

set ct_bytes to crypto.encrypt_aead using "bytes-key", b"` + strings.Repeat(`\x42`, 32) + `"
show crypto.decrypt_aead using ct_bytes, b"` + strings.Repeat(`\x42`, 32) + `"

do together
	set a to crypto.decrypt_aead using ct1, hex_key
	set b to crypto.decrypt_aead using ct2, url_key
end
show a is msg and b is msg

try
	set wrong_key to crypto.random_hex using 32
	set bad1 to crypto.decrypt_aead using ct1, wrong_key
catch err
	show err.code
end
try
	set bad2 to crypto.decrypt_aead using (ct1 joined with "x"), hex_key
catch err
	show err.code
end
try
	set bad3 to crypto.decrypt_aead using "v2:notvalid", hex_key
catch err
	show err.code
end
try
	set bad4 to crypto.encrypt_aead using "secret", "too-short"
catch err
	show err.code
end`
	out := runWithAllEngines(t, src, Options{})
	want := "yes\nyes\nyes\nyes\nyes\nhello\nbytes-key\nyes\nGRAB-E309\nGRAB-E309\nGRAB-E309\nGRAB-E309\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}

	runExpectErrorAllEngines(t, `use crypto
show crypto.encrypt_aead using 123, "`+hexKey+`"`, Options{}, TypeMismatch, "GRAB-E301")
	runExpectErrorAllEngines(t, `use crypto
show crypto.encrypt_aead using "hi", 123`, Options{}, TypeMismatch, "GRAB-E301")

	// Direct byte-key length error.
	badBytesVal, err := stdCryptoEncryptAEAD(nil, lexer.Span{}, []grabrt.Value{grabrt.Text("hi"), grabrt.Bytes([]byte{1, 2, 3})})
	if err == nil {
		t.Fatalf("expected error for 3-byte key, got %v", badBytesVal)
	}
}

func TestParseAllowEnvList(t *testing.T) {
	validCases := []struct {
		raw  string
		want []string
	}{
		{"SESSION_KEY", []string{"SESSION_KEY"}},
		{"SESSION_KEY,OAUTH_SECRET", []string{"SESSION_KEY", "OAUTH_SECRET"}},
		{" _PRIVATE_1 , varTwo , _PRIVATE_1 ", []string{"_PRIVATE_1", "varTwo"}},
	}
	for _, tc := range validCases {
		got, ok := ParseAllowEnvList(tc.raw)
		if !ok {
			t.Fatalf("ParseAllowEnvList(%q) = false, want true", tc.raw)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("ParseAllowEnvList(%q) = %v, want %v", tc.raw, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("ParseAllowEnvList(%q)[%d] = %q, want %q", tc.raw, i, got[i], tc.want[i])
			}
		}
	}

	invalidCases := []string{
		"",
		"   ",
		",",
		"A,,B",
		"*",
		"APP_*",
		"FOO=BAR",
		"1ABC",
		"FOO-BAR",
		"FOO.BAR",
		"FOO BAR",
		"FOO\x00BAR",
	}
	for _, raw := range invalidCases {
		if got, ok := ParseAllowEnvList(raw); ok {
			t.Fatalf("ParseAllowEnvList(%q) = (%v, true), want false", raw, got)
		}
	}
}

func TestEnvCapabilitiesAndLookup(t *testing.T) {
	t.Setenv("GRAB_TEST_ALLOWED", "secret-val-123")
	t.Setenv("GRAB_TEST_EMPTY", "")
	t.Setenv("GRAB_TEST_DENIED", "do-not-expose-me")

	// Default-deny when AllowEnv is empty.
	runExpectErrorAllEngines(t, `use env
show env.get using "GRAB_TEST_ALLOWED"`, Options{}, PermissionDenied, "GRAB-E401")
	runExpectErrorAllEngines(t, `use env
show env.has using "GRAB_TEST_DENIED"`, Options{}, PermissionDenied, "GRAB-E401")

	// Allowed variables work; unset allowed returns nothing/no; empty allowed returns ""/yes.
	allowedOpts := Options{
		Permissions: Permissions{
			AllowEnv: []string{"GRAB_TEST_ALLOWED", "GRAB_TEST_EMPTY", "GRAB_TEST_UNSET"},
		},
	}
	src := `use env
show env.has using "GRAB_TEST_ALLOWED"
show env.get using "GRAB_TEST_ALLOWED"
show env.has using "GRAB_TEST_EMPTY"
show (env.get using "GRAB_TEST_EMPTY") is ""
show env.has using "GRAB_TEST_UNSET"
show (env.get using "GRAB_TEST_UNSET") is nothing
do together
	set t1 to env.get using "GRAB_TEST_ALLOWED"
	set t2 to env.has using "GRAB_TEST_UNSET"
end
show t1
show t2`
	out := runWithAllEngines(t, src, allowedOpts)
	want := "yes\nsecret-val-123\nyes\nyes\nno\nyes\nsecret-val-123\nno\n"
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}

	// Non-allowlisted variables must fail closed with GRAB-E401 and never leak values.
	for _, deniedExpr := range []string{
		`env.get using "GRAB_TEST_DENIED"`,
		`env.has using "GRAB_TEST_DENIED"`,
		`env.get using "grab_test_allowed"`,
		`env.get using "*"`,
		`env.get using ""`,
	} {
		code := "use env\nshow " + deniedExpr
		prog, err := parser.Parse("denied.grab", code)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []EngineMode{EngineAST, EngineVM, EngineAuto} {
			opts := allowedOpts
			opts.Engine = mode
			var buf bytes.Buffer
			err := RunWithOptions(prog, &buf, opts)
			if err == nil {
				t.Fatalf("engine=%s expected GRAB-E401 for %s, got output %q", mode, deniedExpr, buf.String())
			}
			if strings.Contains(err.Error(), "do-not-expose-me") || strings.Contains(err.Error(), "secret-val-123") {
				t.Fatalf("engine=%s leaked secret value in error: %v", mode, err)
			}
			var iErr *Error
			if !errors.As(err, &iErr) || iErr.Kind != PermissionDenied || iErr.Code() != "GRAB-E401" {
				t.Fatalf("engine=%s got %v, want PermissionDenied (GRAB-E401)", mode, err)
			}
		}
	}

	runExpectErrorAllEngines(t, `use env
show env.get using 123`, allowedOpts, TypeMismatch, "GRAB-E301")
	runExpectErrorAllEngines(t, `use env
show env.has using 123`, allowedOpts, TypeMismatch, "GRAB-E301")
}
