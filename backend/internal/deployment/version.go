package deployment

import "strings"

var (
	BuildVersion = "dev"
	BuildCommit  = "unknown"
	BuildTime    = ""
)

type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

func CurrentVersion() VersionInfo {
	version := strings.TrimSpace(BuildVersion)
	if version == "" {
		version = "dev"
	}

	commit := strings.TrimSpace(BuildCommit)
	if commit == "" {
		commit = "unknown"
	}

	return VersionInfo{
		Version:   version,
		Commit:    commit,
		BuildTime: strings.TrimSpace(BuildTime),
	}
}
