package repo

import repoidentity "github.com/ai-efficiency/backend/internal/repoidentity"

type RepoIdentity = repoidentity.RepoIdentity

func DeriveRepoIdentity(remoteURL string) (RepoIdentity, error) {
	return repoidentity.DeriveRepoIdentity(remoteURL)
}

func FallbackRepoIdentity(remoteURL, fullName string) RepoIdentity {
	return repoidentity.FallbackRepoIdentity(remoteURL, fullName)
}
