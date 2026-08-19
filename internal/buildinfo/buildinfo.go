package buildinfo

// Info holds build information that can be set at link-time.
type Info struct {
	Version string
	Commit  string
	Date    string
}

var (
	// These variables can be overridden at link-time with -ldflags
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// Current returns the current build information with defaults.
func Current() Info {
	return Info{
		Version: version,
		Commit:  commit,
		Date:    date,
	}
}
