//go:generate -command enumer go tool enumer
//go:generate enumer -type=SinkBackend -json -text -linecomment -output=sink_backend_enumer.go
package model

// SinkBackend identifies the sink delivery backend for pipeline sink steps.
type SinkBackend int

const (
	SinkBackendUnknown SinkBackend = iota // unknown
	SinkBackendFTP                        // ftp
	SinkBackendDB                         // db
	SinkBackendHTTP                       // http
	SinkBackendMongo                      // mongo
	SinkBackendGit                        // git
	SinkBackendGithub                     // github
	SinkBackendGitlab                     // gitlab
	SinkBackendS3                         // s3
)
