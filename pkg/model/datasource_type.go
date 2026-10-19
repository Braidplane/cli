//go:generate -command enumer go tool enumer
//go:generate enumer -type=DataSourceType -transform=snake -output=datasource_type_string.go -text -linecomment
package model

type DataSourceType int

const (
	DataSourceTypeUnknown         DataSourceType = iota // unknown type, should not be used
	DataSourceTypePostgres                              // postgres
	DataSourceTypeMySQL                                 // mysql
	DataSourceTypeBigQuery                              // bigquery
	DataSourceTypeHTTP                                  // http
	DataSourceTypeMongoDB                               // mongodb
	DataSourceTypeHubSpot                               // hubspot
	DataSourceTypeFTP                                   // ftp
	DataSourceTypeGit                                   // git
	DataSourceTypeGithub                                // github
	DataSourceTypeGitlab                                // gitlab
	DataSourceTypeS3                                    // s3
	DataSourceTypeSnowflake                             // snowflake
	DataSourceTypeGoogleWorkspace                       // google_workspace
)
