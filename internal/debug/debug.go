package debug

// Logf logs a formatted debug message. It is initialized to a no-op;
// main sets it after calling initDebugLogger.
var Logf func(string, ...any) = func(string, ...any) {}
