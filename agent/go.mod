module github.com/jumpserver/pam-clients/agent

go 1.23

require (
	github.com/jumpserver/pam-clients/go v1.0.0
	golang.org/x/sys v0.29.0
)

require github.com/coder/websocket v1.8.15 // indirect

// Agent releases build against the SDK from the same repository commit.
replace github.com/jumpserver/pam-clients/go => ../go
