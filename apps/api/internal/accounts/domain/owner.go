package domain

import "strings"

func IsConfiguredOwner(accountEmail, configuredEmail string) bool {
	return configuredEmail != "" && strings.EqualFold(strings.TrimSpace(accountEmail), strings.TrimSpace(configuredEmail))
}
