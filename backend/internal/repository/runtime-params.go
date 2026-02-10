package repository

import (
	"fmt"

	"github.com/dbulashev/dasha/internal/version"
)

const applicationNameRtParam = "application_name"

var (
	runtimeParams = map[string]string{ //nolint: gochecknoglobals
		applicationNameRtParam: fmt.Sprintf("dasha %s %s", version.GetBuildNumber(), version.GetBuildDate()),
	}
)
