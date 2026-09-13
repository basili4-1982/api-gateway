package dashboard

import "testing"

func TestRedactCredentials(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "http userinfo in error",
			in:   `scrape metrics: Get "http://user:s3cret@127.0.0.1:1/metrics": dial tcp 127.0.0.1:1: connect: connection refused`,
			want: `scrape metrics: Get "http://127.0.0.1:1/metrics": dial tcp 127.0.0.1:1: connect: connection refused`,
		},
		{
			name: "parse error with userinfo",
			in:   `invalid configuration: target.url is invalid for target t1: parse "http://user:s3cret@exa mple.com": invalid character " " in host name`,
			want: `invalid configuration: target.url is invalid for target t1: parse "http://exa mple.com": invalid character " " in host name`,
		},
		{
			name: "https userinfo",
			in:   "GET https://admin:hunter2@metrics.example.com/metrics failed",
			want: "GET https://metrics.example.com/metrics failed",
		},
		{
			name: "password already stripped by http client",
			in:   `scrape metrics: Get "http://user:***@127.0.0.1:1/metrics": dial tcp 127.0.0.1:1: connect: connection refused`,
			want: `scrape metrics: Get "http://127.0.0.1:1/metrics": dial tcp 127.0.0.1:1: connect: connection refused`,
		},
		{
			name: "multiple urls",
			in:   "a http://u1:p1@h1/x and b https://u2:p2@h2/y",
			want: "a http://h1/x and b https://h2/y",
		},
		{
			name: "url without userinfo unchanged",
			in:   "scrape metrics: Get \"http://127.0.0.1:8080/metrics\": connection refused",
			want: "scrape metrics: Get \"http://127.0.0.1:8080/metrics\": connection refused",
		},
		{
			name: "no url unchanged",
			in:   "failed to read config file: open /etc/proxy/config.yaml: no such file or directory",
			want: "failed to read config file: open /etc/proxy/config.yaml: no such file or directory",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactCredentials(tt.in); got != tt.want {
				t.Errorf("redactCredentials() = %q, want %q", got, tt.want)
			}
		})
	}
}
