// Mirror of the Go SanitizeRemote (internal/gitsync/sanitize.go).
// Parses the URL instead of regexing ":" → "@", works for any host
// (GitHub, GitLab, Bitbucket, Codeberg, self-hosted) plus SSH scp-like
// syntax and local paths. Never throws.

export function sanitizeRemote(remote: string): { display: string; hasCreds: boolean } {
  const r = (remote || '').trim();
  if (!r) return { display: '', hasCreds: false };
  const schemeIdx = r.indexOf('://');
  if (schemeIdx >= 0) {
    try {
      const u = new URL(r);
      if (!u.host || !u.username) return { display: r, hasCreds: false };
      const schemeEnd = schemeIdx + 3;
      const at = r.lastIndexOf('@');
      if (at < schemeEnd) return { display: r, hasCreds: false };
      return { display: r.slice(0, schemeEnd) + '***@' + r.slice(at + 1), hasCreds: true };
    } catch {
      // Unparseable — mask any //...@ span.
      const at = r.indexOf('@');
      const sl = at >= 0 ? r.lastIndexOf('//', at) : -1;
      if (at >= 0 && sl >= 0) return { display: r.slice(0, sl + 2) + '***@' + r.slice(at + 1), hasCreds: true };
      return { display: r, hasCreds: false };
    }
  }
  // scp-like [user[:pass]@]host:path — the login name is not a secret.
  const at = r.indexOf('@');
  if (at > 0 && !r.slice(0, at).includes('/')) {
    const rest = r.slice(at + 1);
    if (rest.indexOf(':') > 0) {
      const userinfo = r.slice(0, at);
      const cp = userinfo.indexOf(':');
      if (cp >= 0) return { display: userinfo.slice(0, cp + 1) + '***@' + rest, hasCreds: true };
      return { display: r, hasCreds: false };
    }
  }
  return { display: r, hasCreds: false };
}
