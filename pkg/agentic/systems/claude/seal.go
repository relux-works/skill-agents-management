package claude

// AcceptUnsealedExecGuard implements agentic.UnsealedExecGuard. Claude is
// unsealed in hosted Phase 1: its plans export the closed {"kind":"unsealed"}
// guard and import it back. No other plugin may add this method until its
// phase admits it; Muse in particular must keep refusing the unsealed guard.
func (*System) AcceptUnsealedExecGuard() {}
