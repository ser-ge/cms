package debug

// Logf logs a formatted debug message. It is initialized to a no-op;
// main sets it after calling initDebugLogger.
var Logf func(string, ...any) = func(string, ...any) {}

// Enabled is set to true by main when debug logging is active (CMS_DEBUG).
// TUI code reads this to show extra diagnostic info.
var Enabled bool
