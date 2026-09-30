package gitsync

import (
	"net/url"
	"strings"
)

// SanitizeRemote returns a display-safe remote URL for any git host
// (GitHub, GitLab, Bitbucket, Codeberg, self-hosted, local paths).
//
// It parses the URL instead of regexing for ":" → "@":
//   - scheme:// URLs (https://, http://, ssh://, git://, ...): the whole
//     userinfo (username and/or password) is replaced with "***", so
//     https://user:TOKEN@host/org/repo.git becomes https://***@host/org/repo.git
//   - scp-like syntax (git@host:org/repo.git, no scheme): left as-is — the
//     part before "@" is a login name, not a secret, and SSH uses keys/agent.
//     The rare user:pass@host:path form gets its password masked.
//   - local paths: returned unchanged.
//
// The second return value reports whether embedded credentials were found.
func SanitizeRemote(remote string) (string, bool) {
	r := strings.TrimSpace(remote)
	if r == "" {
		return "", false
	}
	// Scheme-based URL: use a real parser, but rebuild the display string
	// by hand — url.String() percent-encodes "***" as "%2A%2A%2A".
	if strings.Contains(r, "://") {
		u, err := url.Parse(r)
		if err != nil || u.Host == "" {
			// Unparseable — fall back to masking any userinfo-looking part.
			return maskUserinfoFallback(r)
		}
		if u.User == nil {
			return r, false
		}
		schemeEnd := strings.Index(r, "://") + 3
		at := strings.LastIndex(r, "@")
		if at < schemeEnd {
			return r, false
		}
		return r[:schemeEnd] + "***@" + r[at+1:], true
	}
	// scp-like [user[:pass]@]host:path — no scheme, not a local path.
	if at := strings.Index(r, "@"); at > 0 && !strings.Contains(r[:at], "/") {
		rest := r[at+1:]
		if colon := strings.Index(rest, ":"); colon > 0 {
			userinfo := r[:at]
			if cp := strings.Index(userinfo, ":"); cp >= 0 {
				// Password present — mask it, keep the username.
				return userinfo[:cp+1] + "***@" + rest, true
			}
			return r, false
		}
	}
	return r, false
}

// HasEmbeddedCredentials reports whether the remote URL itself carries
// userinfo/secrets (as opposed to relying on the system's git auth).
func HasEmbeddedCredentials(remote string) bool {
	_, has := SanitizeRemote(remote)
	return has
}

// RemoteHasPassword reports whether the remote URL embeds a password
// (userinfo with a password component). A bare username (e.g. the "git"
// in scp-like URLs, or https://user@host/...) is not a secret.
func RemoteHasPassword(remote string) bool {
	r := strings.TrimSpace(remote)
	if r == "" {
		return false
	}
	if strings.Contains(r, "://") {
		u, err := url.Parse(r)
		if err != nil || u.User == nil {
			return false
		}
		_, has := u.User.Password()
		return has
	}
	// scp-like [user[:pass]@]host:path — no scheme.
	if at := strings.Index(r, "@"); at > 0 && !strings.Contains(r[:at], "/") {
		if colon := strings.Index(r[at+1:], ":"); colon > 0 {
			return strings.Contains(r[:at], ":")
		}
	}
	return false
}

// maskUserinfoFallback masks "//...@" when url.Parse fails.
func maskUserinfoFallback(r string) (string, bool) {
	if i := strings.Index(r, "@"); i >= 0 {
		if j := strings.LastIndex(r[:i], "//"); j >= 0 {
			return r[:j+2] + "***@" + r[i+1:], true
		}
	}
	return r, false
}

// safeJoinArgs renders a git argv for error messages without leaking the
// per-command auth header (the Bearer token travels in argv, so a raw
// strings.Join would copy the secret into every error, the UI, MCP
// responses, and the log file).
func safeJoinArgs(args []string) string {
	masked := make([]string, len(args))
	for i, a := range args {
		if strings.HasPrefix(strings.ToLower(a), "http.extraheader=authorization: bearer ") {
			masked[i] = "http.extraHeader=AUTHORIZATION: Bearer ***"
			continue
		}
		if strings.HasPrefix(a, "AUTHORIZATION: Bearer ") {
			masked[i] = "AUTHORIZATION: Bearer ***"
			continue
		}
		masked[i] = a
	}
	joined := strings.Join(masked, " ")
	// Belt and braces: mask any userinfo git echoed into the argv.
	if i := strings.Index(joined, "@"); i >= 0 {
		if j := strings.LastIndex(joined[:i], "//"); j >= 0 {
			// Only mask scheme://...@ spans (leaves scp-like git@host alone
			// unless it carries a password).
			seg := joined[j+2 : i]
			if strings.Contains(seg, ":") || strings.Contains(strings.ToLower(joined), "bearer") {
				joined = joined[:j+2] + "***@" + joined[i+1:]
			}
		}
	}
	return joined
}

// SanitizeOutput scrubs secrets from git command output before it is shown
// in the UI, returned via MCP, or written to logs:
//   1. the stored token (exact match),
//   2. any userinfo embedded in the remote URL,
//   3. any full credential-bearing remote string echoed back by git
//      (e.g. fatal: could not read from 'https://user:pass@host/...').
func SanitizeOutput(out, remote, token string) string {
	remote = strings.TrimSpace(remote)
	s := out
	// Replace the raw credential-bearing remote FIRST (before the token
	// redaction rewrites part of it), then scrub any leftover token.
	if remote != "" {
		if disp, has := SanitizeRemote(remote); has {
			s = strings.ReplaceAll(s, remote, disp)
			// Also catch URL-encoded variants git sometimes prints.
			if enc := url.QueryEscape(remote); enc != remote {
				s = strings.ReplaceAll(s, enc, disp)
			}
			// Mask any user:pass@ occurrence derived from the remote.
			if i := strings.Index(remote, "@"); i >= 0 {
				if j := strings.LastIndex(remote[:i], "//"); j >= 0 {
					secretPart := remote[j+2 : i]
					if secretPart != "" {
						s = strings.ReplaceAll(s, secretPart+"@", "***@")
					}
				}
			}
		}
	}
	s = RedactToken(s, token)
	// Final sweep: a token redaction may have left "user:***@" behind —
	// collapse any remaining scheme://userinfo@ to scheme://***@.
	// (scp-like git@host has no "//" so it is untouched.)
	if m, _ := maskUserinfoFallback(s); m != s {
		// Only collapse when the userinfo contains a mask or colon
		// (avoids touching already-clean https://host/... strings,
		// which have no "@" at all and pass through unchanged).
		s = m
	}
	return s
}
