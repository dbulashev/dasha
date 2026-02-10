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
