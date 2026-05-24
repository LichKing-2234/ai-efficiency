package repoidentity

import "testing"

func TestDeriveRepoIdentityCommonRemoteForms(t *testing.T) {
	for _, remote := range []string{
		"git@repo-host.example.com:org/repo.git",
		"ssh://git@repo-host.example.com/org/repo.git",
		"https://repo-host.example.com/org/repo.git",
	} {
		id, err := Derive(remote)
		if err != nil {
			t.Fatalf("Derive(%q): %v", remote, err)
		}
		if id.RepoKey != "repo-host.example.com/org/repo" {
			t.Fatalf("RepoKey for %q = %q", remote, id.RepoKey)
		}
		if id.FullName != "org/repo" {
			t.Fatalf("FullName for %q = %q", remote, id.FullName)
		}
	}
}

func TestDeriveRepoIdentityBitbucketServerForms(t *testing.T) {
	for _, remote := range []string{
		"https://repo-host.example.com/scm/PROJ/repo.git",
		"https://repo-host.example.com/projects/PROJ/repos/repo",
	} {
		id, err := Derive(remote)
		if err != nil {
			t.Fatalf("Derive(%q): %v", remote, err)
		}
		if id.RepoKey != "repo-host.example.com/proj/repo" {
			t.Fatalf("RepoKey for %q = %q", remote, id.RepoKey)
		}
		if id.FullName != "PROJ/repo" {
			t.Fatalf("FullName for %q = %q", remote, id.FullName)
		}
	}
}
