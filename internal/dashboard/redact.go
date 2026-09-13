package dashboard

import "regexp"

// urlUserinfo matches the scheme plus embedded userinfo of a URL, for example
// the `http://user:pass@` prefix of `Get "http://user:pass@host/metrics": ...`.
// The userinfo class excludes '/', whitespace and '@' so it stops at the first
// '@' that terminates the credentials.
var urlUserinfo = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://)[^/\s@]*@`)

// redactCredentials removes embedded userinfo (credentials) from every
// URL-like substring in s. It is applied to error strings before they reach
// Status.Errors, so config validation and metrics scrape failures cannot leak
// secrets that appear inside a target or metrics URL.
func redactCredentials(s string) string {
	return urlUserinfo.ReplaceAllString(s, "$1")
}
