package geoprobe

// ClickhouseConfig holds ClickHouse connection parameters.
type ClickhouseConfig struct {
	Addr     string
	Database string
	Username string
	Password string
	Secure   bool
}
