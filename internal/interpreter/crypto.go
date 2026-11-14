package interpreter

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

const (
	argon2MemoryKiB       uint32 = 19456
	argon2TimeCost        uint32 = 2
	argon2Threads         uint8  = 1
	argon2KeyLen          uint32 = 32
	argon2SaltLen                = 16
	maxPasswordBytes             = 4096
	minRandomBytes               = 16
	maxRandomBytes               = 64
	maxAEADPlaintextBytes        = 10 << 20
	aeadCiphertextPrefix         = "v1:"
	aeadKeyBytes                 = 32
)

var cryptoFunctions = map[string]stdlibFunc{
	"hash_password":    {arity: 1, fn: stdCryptoHashPassword},
	"verify_password":  {arity: 2, fn: stdCryptoVerifyPassword},
	"random_urlsafe":   {arity: 1, fn: stdCryptoRandomURLSafe},
	"random_hex":       {arity: 1, fn: stdCryptoRandomHex},
	"sha256_hex":       {arity: 1, fn: stdCryptoSHA256Hex},
	"sha256_base64url": {arity: 1, fn: stdCryptoSHA256Base64URL},
	"encrypt_aead":     {arity: 2, fn: stdCryptoEncryptAEAD},
	"decrypt_aead":     {arity: 2, fn: stdCryptoDecryptAEAD},
}

func checkCryptoCanceled(ctx *execContext, span lexer.Span) error {
	if ctx != nil && ctx.ctx != nil {
		if err := ctx.ctx.Err(); err != nil {
			return &Error{Kind: RequestTimeout, Span: span, Err: err}
		}
	}
	return nil
}

func stdCryptoHashPassword(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if err := checkCryptoCanceled(ctx, span); err != nil {
		return grabrt.Value{}, err
	}
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	password := args[0].TextValue()
	if !utf8.ValidString(password) {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("password must be valid UTF-8")}
	}
	if len(password) > maxPasswordBytes {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("password exceeds %d bytes", maxPasswordBytes)}
	}
	salt := make([]byte, argon2SaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	hash := argon2.IDKey([]byte(password), salt, argon2TimeCost, argon2MemoryKiB, argon2Threads, argon2KeyLen)
	defer clear(hash)

	encoded := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argon2MemoryKiB,
		argon2TimeCost,
		argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
	return grabrt.Text(encoded), nil
}

func stdCryptoVerifyPassword(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if err := checkCryptoCanceled(ctx, span); err != nil {
		return grabrt.Value{}, err
	}
	if args[0].Kind() != grabrt.TextKind || args[1].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	password := args[0].TextValue()
	if !utf8.ValidString(password) {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("password must be valid UTF-8")}
	}
	if len(password) > maxPasswordBytes {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("password exceeds %d bytes", maxPasswordBytes)}
	}
	mem, timeCost, threads, salt, expectedHash, err := parseArgon2idPHC(args[1].TextValue())
	if err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	actualHash := argon2.IDKey([]byte(password), salt, timeCost, mem, threads, uint32(len(expectedHash)))
	defer clear(actualHash)
	return grabrt.Boolean(subtle.ConstantTimeCompare(actualHash, expectedHash) == 1), nil
}

func parseArgon2idPHC(encoded string) (mem uint32, timeCost uint32, threads uint8, salt []byte, expectedHash []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, fmt.Errorf("invalid argon2id hash format")
	}
	if !strings.HasPrefix(parts[2], "v=") {
		return 0, 0, 0, nil, nil, fmt.Errorf("missing argon2 version")
	}
	ver, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || ver != argon2.Version {
		return 0, 0, 0, nil, nil, fmt.Errorf("unsupported argon2id version")
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 || !strings.HasPrefix(params[0], "m=") || !strings.HasPrefix(params[1], "t=") || !strings.HasPrefix(params[2], "p=") {
		return 0, 0, 0, nil, nil, fmt.Errorf("invalid argon2id parameters")
	}
	mVal, err := strconv.ParseUint(strings.TrimPrefix(params[0], "m="), 10, 32)
	if err != nil || mVal < 8 || mVal > 524288 {
		return 0, 0, 0, nil, nil, fmt.Errorf("argon2id memory parameter out of range")
	}
	tVal, err := strconv.ParseUint(strings.TrimPrefix(params[1], "t="), 10, 32)
	if err != nil || tVal < 1 || tVal > 32 {
		return 0, 0, 0, nil, nil, fmt.Errorf("argon2id time parameter out of range")
	}
	pVal, err := strconv.ParseUint(strings.TrimPrefix(params[2], "p="), 10, 8)
	if err != nil || pVal < 1 || pVal > 16 {
		return 0, 0, 0, nil, nil, fmt.Errorf("argon2id parallelism parameter out of range")
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return 0, 0, 0, nil, nil, fmt.Errorf("invalid argon2id salt")
	}
	expectedHash, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expectedHash) < 16 || len(expectedHash) > 64 {
		return 0, 0, 0, nil, nil, fmt.Errorf("invalid argon2id digest")
	}
	return uint32(mVal), uint32(tVal), uint8(pVal), salt, expectedHash, nil
}

func randomCryptoBytes(span lexer.Span, arg grabrt.Value) ([]byte, error) {
	if arg.Kind() != grabrt.IntegerKind {
		return nil, &Error{Kind: TypeMismatch, Span: span}
	}
	iv := arg.IntegerValue()
	if !iv.IsInt64() {
		return nil, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("byte_len must be between %d and %d", minRandomBytes, maxRandomBytes)}
	}
	n := iv.Int64()
	if n < minRandomBytes || n > maxRandomBytes {
		return nil, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("byte_len must be between %d and %d", minRandomBytes, maxRandomBytes)}
	}
	buf := make([]byte, int(n))
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	return buf, nil
}

func stdCryptoRandomURLSafe(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if err := checkCryptoCanceled(ctx, span); err != nil {
		return grabrt.Value{}, err
	}
	buf, err := randomCryptoBytes(span, args[0])
	if err != nil {
		return grabrt.Value{}, err
	}
	defer clear(buf)
	return grabrt.Text(base64.RawURLEncoding.EncodeToString(buf)), nil
}

func stdCryptoRandomHex(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if err := checkCryptoCanceled(ctx, span); err != nil {
		return grabrt.Value{}, err
	}
	buf, err := randomCryptoBytes(span, args[0])
	if err != nil {
		return grabrt.Value{}, err
	}
	defer clear(buf)
	return grabrt.Text(hex.EncodeToString(buf)), nil
}

func cryptoInputBytes(span lexer.Span, v grabrt.Value) ([]byte, error) {
	switch v.Kind() {
	case grabrt.TextKind:
		return []byte(v.TextValue()), nil
	case grabrt.BytesKind:
		return v.BytesValue(), nil
	default:
		return nil, &Error{Kind: TypeMismatch, Span: span}
	}
}

func stdCryptoSHA256Hex(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if err := checkCryptoCanceled(ctx, span); err != nil {
		return grabrt.Value{}, err
	}
	data, err := cryptoInputBytes(span, args[0])
	if err != nil {
		return grabrt.Value{}, err
	}
	sum := sha256.Sum256(data)
	return grabrt.Text(hex.EncodeToString(sum[:])), nil
}

func stdCryptoSHA256Base64URL(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if err := checkCryptoCanceled(ctx, span); err != nil {
		return grabrt.Value{}, err
	}
	data, err := cryptoInputBytes(span, args[0])
	if err != nil {
		return grabrt.Value{}, err
	}
	sum := sha256.Sum256(data)
	return grabrt.Text(base64.RawURLEncoding.EncodeToString(sum[:])), nil
}

func decodeAEADKey(span lexer.Span, v grabrt.Value) ([]byte, error) {
	switch v.Kind() {
	case grabrt.BytesKind:
		raw := v.BytesValue()
		if len(raw) != aeadKeyBytes {
			return nil, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("invalid AES-256 key length %d: expected %d bytes", len(raw), aeadKeyBytes)}
		}
		out := make([]byte, aeadKeyBytes)
		copy(out, raw)
		return out, nil
	case grabrt.TextKind:
		s := v.TextValue()
		switch len(s) {
		case 64:
			if b, err := hex.DecodeString(s); err == nil && len(b) == aeadKeyBytes {
				return b, nil
			}
		case 43, 44:
			for _, enc := range []*base64.Encoding{
				base64.RawURLEncoding,
				base64.URLEncoding,
				base64.StdEncoding,
				base64.RawStdEncoding,
			} {
				if b, err := enc.DecodeString(s); err == nil && len(b) == aeadKeyBytes {
					return b, nil
				}
			}
		case aeadKeyBytes:
			out := make([]byte, aeadKeyBytes)
			copy(out, s)
			return out, nil
		}
		return nil, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("invalid AES-256 key: expected 32 raw bytes, 64-char hex, or 32-byte base64/base64url")}
	default:
		return nil, &Error{Kind: TypeMismatch, Span: span}
	}
}

func stdCryptoEncryptAEAD(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if err := checkCryptoCanceled(ctx, span); err != nil {
		return grabrt.Value{}, err
	}
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	plaintext := args[0].TextValue()
	if !utf8.ValidString(plaintext) {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("plaintext must be valid UTF-8")}
	}
	if len(plaintext) > maxAEADPlaintextBytes {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("plaintext exceeds %d bytes", maxAEADPlaintextBytes)}
	}
	key, err := decodeAEADKey(span, args[1])
	if err != nil {
		return grabrt.Value{}, err
	}
	defer clear(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	nonceSize := gcm.NonceSize()
	buf := make([]byte, nonceSize, nonceSize+len(plaintext)+gcm.Overhead())
	if _, err := io.ReadFull(rand.Reader, buf[:nonceSize]); err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	sealed := gcm.Seal(buf, buf[:nonceSize], []byte(plaintext), nil)
	return grabrt.Text(aeadCiphertextPrefix + base64.RawURLEncoding.EncodeToString(sealed)), nil
}

func stdCryptoDecryptAEAD(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if err := checkCryptoCanceled(ctx, span); err != nil {
		return grabrt.Value{}, err
	}
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	ciphertext := args[0].TextValue()
	if !strings.HasPrefix(ciphertext, aeadCiphertextPrefix) {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("invalid aead ciphertext format")}
	}
	payload := strings.TrimPrefix(ciphertext, aeadCiphertextPrefix)
	if len(payload) > base64.RawURLEncoding.EncodedLen(maxAEADPlaintextBytes+64) {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("ciphertext exceeds size limit")}
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("invalid aead ciphertext encoding")}
	}
	key, err := decodeAEADKey(span, args[1])
	if err != nil {
		return grabrt.Value{}, err
	}
	defer clear(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: err}
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize+gcm.Overhead() {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("aead decryption failed")}
	}
	plain, err := gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("aead decryption failed")}
	}
	if !utf8.Valid(plain) {
		clear(plain)
		return grabrt.Value{}, &Error{Kind: StandardLibraryError, Span: span, Err: fmt.Errorf("decrypted payload is not valid UTF-8")}
	}
	res := string(plain)
	clear(plain)
	return grabrt.Text(res), nil
}
