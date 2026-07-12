package version

var (
	buildDate   = "BUILD_DATE"   //nolint:gochecknoglobals
	buildNumber = "BUILD_NUMBER" //nolint:gochecknoglobals
)

func GetBuildDate() string {
	return buildDate
}

func GetBuildNumber() string {
	return buildNumber
}

// Resolved returns the stamped release build number, or "dev" when it was not set
// by the release ldflags (buildNumber still holds its unset sentinel). Keeps the
// sentinel value private to this package.
func Resolved() string {
	if buildNumber == "BUILD_NUMBER" {
		return "dev"
	}

	return buildNumber
}
