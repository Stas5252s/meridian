package version

// Injected at build time with -ldflags -X.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)
