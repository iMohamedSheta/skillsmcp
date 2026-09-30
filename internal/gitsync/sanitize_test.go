package gitsync

import (
	"strings"
	"testing"
)

func TestSanitizeRemote(t *testing.T) {
	cases := []struct {
		in       string
		want     string
		hasCreds bool
	}{
		{"https://github.com/org/skills.git", "https://github.com/org/skills.git", false},
		{"https://user:TOKEN123@github.com/org/skills.git", "https://***@github.com/org/skills.git", true},
		{"https://oauth2:TOKEN@gitlab.com/org/skills.git", "https://***@gitlab.com/org/skills.git", true},
		{"https://user@bitbucket.org/team/repo.git", "https://***@bitbucket.org/team/repo.git", true},
		{"git@github.com:org/skills.git", "git@github.com:org/skills.git", false},
		{"git@gitlab.com:org/skills.git", "git@gitlab.com:org/skills.git", false},
		{"ssh://git@codeberg.org/org/skills.git", "ssh://***@codeberg.org/org/skills.git", true},
		{"/srv/git/skills.git", "/srv/git/skills.git", false},
		{"./relative/path.git", "./relative/path.git", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, has := SanitizeRemote(c.in)
		if got != c.want || has != c.hasCreds {
			t.Errorf("SanitizeRemote(%q) = (%q, %v), want (%q, %v)", c.in, got, has, c.want, c.hasCreds)
		}
		if c.hasCreds && strings.Contains(got, "TOKEN") {
			t.Errorf("SanitizeRemote(%q) leaked secret: %q", c.in, got)
		}
	}
}

func TestSanitizeOutput(t *testing.T) {
	remote := "https://user:s3kret@github.com/org/skills.git"
	out := "fatal: could not read from '" + remote + "' (auth failed with s3kret)"
	clean := SanitizeOutput(out, remote, "s3kret")
	if strings.Contains(clean, "s3kret") {
		t.Fatalf("token leaked: %q", clean)
	}
	if !strings.Contains(clean, "https://***@github.com/org/skills.git") {
		t.Fatalf("want sanitized remote, got %q", clean)
	}
}

func TestSafeJoinArgsMasksBearer(t *testing.T) {
	args := []string{"-c", "http.extraHeader=AUTHORIZATION: Bearer s3kret", "push", "-u", "origin", "main"}
	joined := safeJoinArgs(args)
	if strings.Contains(joined, "s3kret") {
		t.Fatalf("bearer leaked: %q", joined)
	}
	if !strings.Contains(joined, "Bearer ***") {
		t.Fatalf("want masked bearer, got %q", joined)
	}
}

func TestRemoteHasPassword(t *testing.T) {
	if !RemoteHasPassword("https://user:pass@host/org/repo.git") {
		t.Fatal("want true for user:pass@")
	}
	if RemoteHasPassword("https://user@host/org/repo.git") {
		t.Fatal("bare username is not a password")
	}
	if RemoteHasPassword("git@github.com:org/repo.git") {
		t.Fatal("scp login name is not a password")
	}
	if !RemoteHasPassword("user:pass@host:path") {
		t.Fatal("want true for scp user:pass@")
	}
}
