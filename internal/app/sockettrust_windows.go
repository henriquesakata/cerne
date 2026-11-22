package app

// socketTrust never admits a socket on Windows: the wake does not run there
// (its inbox demands the auth line, which needs the session's token).
func socketTrust(string) string { return WakeNoWake }
