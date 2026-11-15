package obs

import (
	"log/slog"
	"os"
)

// NewLogger returns a JSON logger with service identity attached to every
// record. JSON keeps log aggregation cheap and machine-parseable.
func NewLogger(level slog.Level, version, commit string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				attr.Value = slog.StringValue(attr.Value.Time().UTC().Format("2006-01-02T15:04:05.000Z07:00"))
			}
			return attr
		},
	})

	return slog.New(handler).With(
		slog.String("service", "checkout-api"),
		slog.String("version", version),
		slog.String("commit", commit),
	)
}
