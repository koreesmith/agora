package domains

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Verification methods, per the AT Proto handle-resolution spec. Both prove
// the same thing (the claimant controls the domain); which one a user reaches
// for depends on whether they can edit DNS or only serve a file.
const (
	MethodDNS       = "dns"
	MethodWellKnown = "well-known"
)

// dnsRecordName is the subdomain the TXT record lives at, and wellKnownPath
// the file path the HTTPS alternative is served from — both fixed by the AT
// Proto spec, not Agora's choice, so a domain already verified with Bluesky
// itself needs no new records to work here.
const (
	dnsRecordName = "_atproto."
	wellKnownPath = "/.well-known/atproto-did"
)

// staleClaimWindow is how long an unverified claim holds a domain against
// other accounts before anyone else may take it over (AGORA-290). Without it,
// claiming a domain you don't own would be a free, permanent block on the
// person who actually does own it — the claim itself is unauthenticated by
// definition, since proving ownership is the step that hasn't happened yet.
const staleClaimWindow = 7 * 24 * time.Hour

// verifyTimeout bounds a single verification attempt. Deliberately short: a
// user is watching this happen from the settings panel, and a domain whose
// DNS or web server takes longer than this to answer is not one we should
// bind a handle to anyway.
const verifyTimeout = 8 * time.Second

// verifyDomain checks whether domain proves ownership of did by either
// supported method, returning which one succeeded. DNS is tried first: it's
// cheaper, it's what the spec presents first, and unlike the HTTPS method it
// keeps working when the domain has no web server at all (a bare domain a
// user bought purely to be their handle).
//
// The two errors are joined on failure rather than reporting only the last
// one — a user who set up neither method needs to see both explanations, and
// a user who set up one of them needs to see why that one specifically didn't
// take, not why the method they ignored didn't either.
func (s *Service) verifyDomain(ctx context.Context, domain, did string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	dnsErr := verifyDNS(ctx, domain, did)
	if dnsErr == nil {
		return MethodDNS, nil
	}
	wkErr := verifyWellKnown(ctx, domain, did)
	if wkErr == nil {
		return MethodWellKnown, nil
	}
	return "", fmt.Errorf("DNS: %v; well-known file: %v", dnsErr, wkErr)
}

// verifyDNS looks for a TXT record at _atproto.<domain> whose value is
// "did=<did>". Every record at that name is checked, not just the first:
// a domain routinely carries several TXT records at one name (SPF, other
// services' verification tokens), and which order the resolver returns them
// in is not something the user controls.
func verifyDNS(ctx context.Context, domain, did string) error {
	records, err := net.DefaultResolver.LookupTXT(ctx, dnsRecordName+domain)
	if err != nil {
		return fmt.Errorf("could not look up %s%s (%v)", dnsRecordName, domain, dnsErrText(err))
	}
	for _, rec := range records {
		if strings.TrimSpace(rec) == "did="+did {
			return nil
		}
	}
	if len(records) == 0 {
		return fmt.Errorf("no TXT records found at %s%s", dnsRecordName, domain)
	}
	return fmt.Errorf("found %d TXT record(s) at %s%s, but none matched \"did=%s\"",
		len(records), dnsRecordName, domain, did)
}

// dnsErrText unwraps net.DNSError into something a non-operator can act on.
// The raw error text ("lookup _atproto.example.com on 10.0.0.1:53: no such
// host") leaks the instance's resolver address into a user-facing status
// message and buries the one bit that matters.
func dnsErrText(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsNotFound:
			return "no such record"
		case dnsErr.IsTimeout:
			return "the lookup timed out"
		}
	}
	return "lookup failed"
}

// verifyWellKnown fetches https://<domain>/.well-known/atproto-did and checks
// the body is exactly the DID. Redirects are refused rather than followed:
// the spec disallows them, and honoring one would let a domain delegate its
// proof of ownership to a host that isn't the domain, which is the whole
// thing this check exists to establish.
func verifyWellKnown(ctx context.Context, domain, did string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://"+domain+wellKnownPath, nil)
	if err != nil {
		return fmt.Errorf("invalid domain")
	}
	req.Header.Set("User-Agent", "Agora")

	resp, err := verifyHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach https://%s%s", domain, wellKnownPath)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound ||
		resp.StatusCode == http.StatusSeeOther || resp.StatusCode == http.StatusTemporaryRedirect ||
		resp.StatusCode == http.StatusPermanentRedirect {
		return fmt.Errorf("https://%s%s redirects — the file must be served by the domain itself", domain, wellKnownPath)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("https://%s%s returned HTTP %d", domain, wellKnownPath, resp.StatusCode)
	}

	// A DID is a couple hundred bytes at most; the cap stops a hostile or
	// misconfigured host from streaming a response body at us indefinitely.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if err != nil {
		return fmt.Errorf("could not read https://%s%s", domain, wellKnownPath)
	}
	got := strings.TrimSpace(string(body))
	if got != did {
		if got == "" {
			return fmt.Errorf("the file at https://%s%s is empty", domain, wellKnownPath)
		}
		return fmt.Errorf("the file at https://%s%s contains a different DID", domain, wellKnownPath)
	}
	return nil
}

// ── SSRF protection ───────────────────────────────────────────────────────────
//
// The well-known fetch targets a domain the user typed, so it needs the same
// protection internal/federation's fedHTTPClient gives outbound federation
// requests: without it, claiming "localhost" or an internal hostname would
// turn verification into a request-forgery primitive against services behind
// the instance. Reimplemented here rather than exported from that package —
// the same tradeoff internal/atproto's own domainFromURL already makes, since
// a shared 20-line helper isn't worth a dependency from this package to the
// ActivityPub one it has nothing else to do with.

var verifyHTTPClient = &http.Client{
	Timeout:   verifyTimeout,
	Transport: &http.Transport{DialContext: safeDialContext},
	// Refuse rather than follow — see verifyWellKnown.
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// isPublicIP reports whether ip is globally routable and therefore something
// we're willing to connect to. Loopback, private, link-local (including the
// cloud metadata range), CGNAT, unspecified, and multicast are all refused.
func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// 100.64.0.0/10 — carrier-grade NAT, not covered by IsPrivate.
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
		return false
	}
	return true
}

// safeDialContext resolves the target host, verifies every resolved address is
// public, then dials a validated IP directly — closing the DNS-rebinding
// window between the check and the connection.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ipa := range ips {
		if !isPublicIP(ipa.IP) {
			return nil, fmt.Errorf("refusing to connect to non-public address %s", ipa.IP)
		}
	}
	d := &net.Dialer{Timeout: verifyTimeout}
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}
