package federation

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HTTP Signatures (draft-cavage) — the scheme Mastodon and the rest of the
// fediverse actually verify. Distinct from the older custom protocol's
// embedded-JSON-field Ed25519 signature (see verifyActivity in federation.go),
// which only Agora-to-Agora federation understands.

const signedHeaderList = "(request-target) host date digest"

// signRequest computes the Digest and Signature headers for an outbound
// ActivityPub delivery and sets them on req. body must be the exact bytes
// that will be sent as the request body.
func signRequest(req *http.Request, keyID string, privKey *rsa.PrivateKey, body []byte) error {
	digest := sha256.Sum256(body)
	req.Header.Set("Digest", "SHA-256="+base64.StdEncoding.EncodeToString(digest[:]))
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("Host", req.URL.Host)
	req.Host = req.URL.Host

	signingString := buildSigningString(req, strings.Fields(signedHeaderList))

	hashed := sha256.Sum256([]byte(signingString))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hashed[:])
	if err != nil {
		return fmt.Errorf("sign request: %w", err)
	}

	req.Header.Set("Signature", fmt.Sprintf(
		`keyId="%s",algorithm="rsa-sha256",headers="%s",signature="%s"`,
		keyID, signedHeaderList, base64.StdEncoding.EncodeToString(sig),
	))
	return nil
}

// buildSigningString reconstructs the draft-cavage signing string from the
// given header names, pulling actual values off req (or its pseudo-headers).
func buildSigningString(req *http.Request, headers []string) string {
	lines := make([]string, 0, len(headers))
	for _, h := range headers {
		switch strings.ToLower(h) {
		case "(request-target)":
			lines = append(lines, fmt.Sprintf("(request-target): %s %s",
				strings.ToLower(req.Method), req.URL.RequestURI()))
		case "host":
			host := req.Host
			if host == "" {
				host = req.URL.Host
			}
			lines = append(lines, "host: "+host)
		default:
			lines = append(lines, strings.ToLower(h)+": "+req.Header.Get(h))
		}
	}
	return strings.Join(lines, "\n")
}

type sigParams struct {
	keyID     string
	algorithm string
	headers   []string
	signature []byte
}

// parseSignatureHeader parses a draft-cavage Signature header value, e.g.:
//
//	keyId="https://example.com/users/alice#main-key",algorithm="rsa-sha256",headers="(request-target) host date digest",signature="base64..."
func parseSignatureHeader(v string) (*sigParams, error) {
	if v == "" {
		return nil, errors.New("missing Signature header")
	}
	fields := map[string]string{}
	for _, part := range splitSignatureFields(v) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), `"`)
		fields[key] = val
	}
	if fields["keyId"] == "" || fields["signature"] == "" {
		return nil, errors.New("signature header missing keyId or signature")
	}
	sig, err := base64.StdEncoding.DecodeString(fields["signature"])
	if err != nil {
		return nil, fmt.Errorf("invalid signature encoding: %w", err)
	}
	headers := strings.Fields(fields["headers"])
	if len(headers) == 0 {
		headers = []string{"date"}
	}
	return &sigParams{
		keyID:     fields["keyId"],
		algorithm: fields["algorithm"],
		headers:   headers,
		signature: sig,
	}, nil
}

// splitSignatureFields splits a comma-separated key="value" list without
// breaking on commas embedded inside quoted values (e.g. the headers field).
func splitSignatureFields(v string) []string {
	var parts []string
	var cur strings.Builder
	inQuotes := false
	for _, r := range v {
		switch r {
		case '"':
			inQuotes = !inQuotes
			cur.WriteRune(r)
		case ',':
			if inQuotes {
				cur.WriteRune(r)
			} else {
				parts = append(parts, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// verifyInboundSignature validates the HTTP Signature on an inbound
// ActivityPub request. It dereferences the signer's actor document (via the
// SSRF-safe fedHTTPClient) to obtain their public key, then verifies both
// the signature and that the Digest header matches the actual body. On
// success it returns the verified actor URL (the keyId with any #fragment
// stripped) — callers must treat this, not any unsigned "actor"/"attributedTo"
// field in the request body, as the trustworthy signer identity.
func verifyInboundSignature(r *http.Request, body []byte) (string, error) {
	sp, err := parseSignatureHeader(r.Header.Get("Signature"))
	if err != nil {
		return "", err
	}

	// Digest must match the actual body, independent of whether "digest" was
	// a signed header — otherwise a signed-but-stale Digest could be paired
	// with a swapped body.
	if digestHeader := r.Header.Get("Digest"); digestHeader != "" {
		sum := sha256.Sum256(body)
		want := "SHA-256=" + base64.StdEncoding.EncodeToString(sum[:])
		if !strings.EqualFold(digestHeader, want) {
			return "", errors.New("digest mismatch")
		}
	}

	pubKey, err := fetchActorPublicKey(sp.keyID)
	if err != nil {
		return "", fmt.Errorf("fetch actor public key: %w", err)
	}

	signingString := buildSigningString(r, sp.headers)
	hashed := sha256.Sum256([]byte(signingString))
	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hashed[:], sp.signature); err != nil {
		return "", fmt.Errorf("signature verification failed: %w", err)
	}
	return strings.SplitN(sp.keyID, "#", 2)[0], nil
}

// fetchActorPublicKey dereferences an actor (or actor#key) URL and extracts
// its publicKeyPem.
func fetchActorPublicKey(keyID string) (*rsa.PublicKey, error) {
	actorURL := strings.SplitN(keyID, "#", 2)[0]
	req, err := http.NewRequest(http.MethodGet, actorURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/activity+json")
	resp, err := fedHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("actor fetch returned %d", resp.StatusCode)
	}

	var actor struct {
		PublicKey struct {
			PublicKeyPem string `json:"publicKeyPem"`
		} `json:"publicKey"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&actor); err != nil {
		return nil, err
	}
	if actor.PublicKey.PublicKeyPem == "" {
		return nil, errors.New("actor has no publicKeyPem")
	}
	return parseRSAPublicKeyPEM(actor.PublicKey.PublicKeyPem)
}

func parseRSAPublicKeyPEM(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("invalid PEM block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("public key is not RSA")
	}
	return rsaPub, nil
}

func parseRSAPrivateKeyPEM(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("invalid PEM block")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return rsaKey, nil
}
