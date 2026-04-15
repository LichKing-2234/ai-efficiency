package repo

import "testing"

func TestDeriveRepoIdentityNormalizesCommonRemotes(t *testing.T) {
	cases := []struct {
		name     string
		remote   string
		repoKey  string
		fullName string
		repoName string
	}{
		{
			name:     "github https",
			remote:   "https://github.com/acme/platform.git",
			repoKey:  "github.com/acme/platform",
			fullName: "acme/platform",
			repoName: "platform",
		},
		{
			name:     "github ssh",
			remote:   "git@github.com:acme/platform.git",
			repoKey:  "github.com/acme/platform",
			fullName: "acme/platform",
			repoName: "platform",
		},
		{
			name:     "bitbucket http clone",
			remote:   "https://bitbucket.example.com/scm/PROJ/platform.git",
			repoKey:  "bitbucket.example.com/proj/platform",
			fullName: "PROJ/platform",
			repoName: "platform",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DeriveRepoIdentity(tc.remote)
			if err != nil {
				t.Fatalf("DeriveRepoIdentity(%q): %v", tc.remote, err)
			}
			if got.RepoKey != tc.repoKey {
				t.Fatalf("RepoKey = %q, want %q", got.RepoKey, tc.repoKey)
			}
			if got.FullName != tc.fullName {
				t.Fatalf("FullName = %q, want %q", got.FullName, tc.fullName)
			}
			if got.Name != tc.repoName {
				t.Fatalf("Name = %q, want %q", got.Name, tc.repoName)
			}
		})
	}
}
