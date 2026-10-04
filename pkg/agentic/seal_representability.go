package agentic

import "unicode/utf8"

// guardStringsRepresentable is the common lossless-JSON predicate for
// producer inputs, raw wire and typed guards. Never normalize the bytes:
// encoding/json would silently replace invalid UTF-8 with U+FFFD.
func guardStringsRepresentable(values ...string) bool {
	for _, value := range values {
		if !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func sealRepresentable(s Seal) bool {
	return guardStringsRepresentable(s.Schema, s.SchemaVersion, s.Data.Kind) &&
		processBindingRepresentable(s.Data.Binding) &&
		(s.Data.Sealed == nil || sealedDataRepresentable(*s.Data.Sealed))
}

func processBindingRepresentable(b *ProcessBinding) bool {
	if b == nil {
		return true
	}
	if !guardStringsRepresentable(b.Binary, b.KeyID, b.Digest) ||
		!guardStringsRepresentable(b.Argv...) || !guardStringsRepresentable(b.EnvNames...) {
		return false
	}
	for name, value := range b.Selectors {
		if !guardStringsRepresentable(name, value) {
			return false
		}
	}
	return true
}

func sealedDataRepresentable(data SealedData) bool {
	if !guardStringsRepresentable(data.System, data.Binary, data.Digest) ||
		!guardStringsRepresentable(data.Argv...) || !processBindingRepresentable(data.Binding) {
		return false
	}
	for _, artifact := range data.Artifacts {
		if !guardStringsRepresentable(artifact.Name, artifact.Path, artifact.Digest) {
			return false
		}
	}
	for name, value := range data.Selectors {
		if !guardStringsRepresentable(name, value) {
			return false
		}
	}
	return true
}
