package config

import "fmt"

// validate checks the expanded Config for required fields.
func validate(c Config) error {
	if c.ListenTCP == "" && c.ListenUnix == "" {
		return fmt.Errorf("config: at least one of listen_tcp or listen_unix must be set")
	}
	for i, r := range c.Routes {
		if r.Path == "" {
			return fmt.Errorf("config: route[%d]: path is required", i)
		}
		if r.Upstream == "" && len(r.CredentialCommand) == 0 {
			return fmt.Errorf("config: route[%d]: upstream or credential_command is required", i)
		}
	}
	return nil
}
