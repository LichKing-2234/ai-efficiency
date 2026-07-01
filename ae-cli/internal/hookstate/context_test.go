package hookstate

import "testing"

func TestContextCacheKeyIncludesServerSubjectAndRepo(t *testing.T) {
	a := Context{ServerURL: "https://AE.example.com/", AuthSubject: "user:1", RepoKey: "repo-host.example.com/org/repo"}
	b := Context{ServerURL: "https://ae.example.com", AuthSubject: "user:2", RepoKey: "repo-host.example.com/org/repo"}
	if !a.Stable() {
		t.Fatalf("context should be stable")
	}
	if a.Normalized().ServerURL != "https://ae.example.com" {
		t.Fatalf("normalized server = %q", a.Normalized().ServerURL)
	}
	if a.CacheKey() == b.CacheKey() {
		t.Fatalf("cache key must change by auth subject")
	}
}
