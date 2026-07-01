package attributionlocal

import "github.com/ai-efficiency/ae-cli/internal/clistate"

func AttributionRootDir() string {
	return clistate.AttributionStateDir()
}

var SaveJSON = clistate.SaveJSON
var LoadJSON = clistate.LoadJSON
