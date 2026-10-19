package transport

import (
	"fmt"
	"net/url"
)

// AllowedEndpoint permits the official vendor endpoint or a destination
// explicitly added by trusted application code. Untrusted runtime JSON cannot
// modify this allowlist.
func AllowedEndpoint(endpoint, official string, additional []string) error {
	reference, err := url.Parse(official)
	if err != nil || reference.Scheme == "" {
		return fmt.Errorf("invalid official provider endpoint")
	}
	if err := Endpoint(endpoint, reference.Scheme); err != nil {
		return err
	}
	if endpoint == official {
		return nil
	}
	for _, candidate := range additional {
		if endpoint == candidate {
			return nil
		}
	}
	return fmt.Errorf("provider endpoint is not approved")
}
