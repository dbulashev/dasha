package config

import "os"

// ResolveSecret returns the value of the variable env when env is set, else plain.
// unset reports that env is set but the variable is empty or missing.
func ResolveSecret(plain, env string) (value string, unset bool) {
	if env == "" {
		return plain, false
	}

	value = os.Getenv(env)

	return value, value == ""
}
