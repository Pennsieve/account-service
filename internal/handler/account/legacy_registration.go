package account

import (
	errs "errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ErrLegacyRegistrationRetired refuses a registration without an external ID
// once LEGACY_REGISTRATION_CUTOFF has passed.
var ErrLegacyRegistrationRetired = errs.New("Registering without an external ID is no longer supported. Update the Pennsieve agent (pennsieve account register), or connect the account in the app: Analysis → Compute Nodes → Connect AWS account.")

// legacyRegistrationCutoff is when registrations without an external ID stop
// being accepted (LEGACY_REGISTRATION_CUTOFF, RFC 3339), and whether one is
// set. A value that doesn't parse counts as unset: a typo in the config must
// never block registration.
func legacyRegistrationCutoff() (time.Time, bool) {
	v := strings.TrimSpace(os.Getenv("LEGACY_REGISTRATION_CUTOFF"))
	if v == "" {
		return time.Time{}, false
	}
	cutoff, err := time.Parse(time.RFC3339, v)
	if err != nil {
		log.Printf("LEGACY_REGISTRATION_CUTOFF %q isn't an RFC 3339 time, so registrations without an external ID stay accepted: %v", v, err)
		return time.Time{}, false
	}
	return cutoff, true
}

// legacyRegistration decides on a registration without an external ID at now:
// refused once the cutoff has passed; before it, accepted with the headers
// that tell a client it's going away (Deprecation, and Sunset with the date).
// With no cutoff, accepted as before, with no headers.
func legacyRegistration(now time.Time) (refused bool, headers map[string]string) {
	cutoff, ok := legacyRegistrationCutoff()
	if !ok {
		return false, nil
	}
	if !now.Before(cutoff) {
		return true, nil
	}
	return false, map[string]string{
		"Deprecation": "true",
		"Sunset":      cutoff.UTC().Format(http.TimeFormat),
	}
}
