package alerts

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateDocsURL reports whether docsURL can be the customer docs' address,
// which the console joins to pages in links and the provisioner puts into
// Alertmanager's templates: an absolute HTTP or HTTPS URL without a query or
// fragment, as a page follows it, and with nothing a template or Slack's
// link markup would read as its own.
func ValidateDocsURL(docsURL string) error {
	u, err := url.Parse(docsURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(docsURL, "{}|<>?#&\\ \t\n\"'") {
		return fmt.Errorf("the docs' address %q is no plain http or https URL", docsURL)
	}
	return nil
}
