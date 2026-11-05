package testkit

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"testing"

	"github.com/biairmal/go-sdk/lib/auth"
)

// signingKeyBits is the smallest RSA key go-identity accepts. A larger key
// would only slow the tests down.
const signingKeyBits = 2048

// testKID is the kid on every test token and on the one key in the JWKS.
const testKID = "test-key"

// Keys stands in for go-identity's token signing: an RS256 key whose public
// half is served as a JWKS on a local port, and an issuer that signs tokens
// with it. Point auth.local.jwks_url at JWKSURL and the service accepts
// tokens from Token exactly as it accepts identity's.
type Keys struct {
	// JWKSURL serves the key set, like identity's /.well-known/jwks.json.
	JWKSURL string
	issuer  auth.Issuer
}

// SigningKeys generates a key, serves its JWKS for the duration of the test,
// and returns an issuer for tokens with the given iss and aud claims.
func SigningKeys(t *testing.T, issuer, audience string) *Keys {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, signingKeyBits)
	if err != nil {
		t.Fatalf("testkit: generate key: %v", err)
	}
	jwks := auth.JWKSHandler(func() []auth.PublicKey {
		return []auth.PublicKey{{KID: testKID, Key: &key.PublicKey}}
	})
	srv := Server(t, jwks)
	iss, err := auth.NewRS256Issuer(key,
		auth.WithIssuerName(issuer), auth.WithDefaultAudience(audience), auth.WithKeyID(testKID))
	if err != nil {
		t.Fatalf("testkit: build issuer: %v", err)
	}
	return &Keys{JWKSURL: srv.URL + "/.well-known/jwks.json", issuer: iss}
}

// Token signs an access token for subject with identity's claims: tid (the
// tenant) and roles, plus any extra claims.
func (k *Keys) Token(t *testing.T, subject, tenantID string, roles []string, extra map[string]any) string {
	t.Helper()
	claims := map[string]any{"tid": tenantID}
	for name, v := range extra {
		claims[name] = v
	}
	tok, err := k.issuer.Issue(subject, auth.WithRoles(roles...), auth.WithExtraClaims(claims))
	if err != nil {
		t.Fatalf("testkit: sign token: %v", err)
	}
	return tok
}

// Bearer is the Authorization header carrying token.
func Bearer(token string) http.Header {
	return http.Header{"Authorization": {"Bearer " + token}}
}
