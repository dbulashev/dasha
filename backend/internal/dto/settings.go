package dto

type PgSetting struct {
	Name    string
	Setting string
	Unit    string
	Source  string
}

type SettingsNotification struct {
	Key    string
	Params map[string]string
}
