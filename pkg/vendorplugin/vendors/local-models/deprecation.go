package localmodels

import "path/filepath"

// importerName is the migration entry point a deprecation warning names. The
// operator runs it once to move legacy rows into the Curator catalog; the
// launch path never migrates implicitly.
const importerName = "runtimecatalog.ImportAt"

// DeprecationWarning is the typed warning emitted when the legacy
// local-models.toml is present. It names the source basename and the importer
// that replaces it. It carries no file contents, values, or paths.
type DeprecationWarning struct {
	// Source is the legacy basename, always "local-models.toml".
	Source string
	// Importer is the migration entry point, always runtimecatalog.ImportAt.
	Importer string
}

// Warning reports the sanitized deprecation message.
func (w *DeprecationWarning) Warning() string {
	if w == nil {
		return ""
	}
	return "localmodels: " + filepath.Base(w.Source) + " is deprecated; import it with " + w.Importer
}

// IsDeprecated reports whether a warning is present.
func (w *DeprecationWarning) IsDeprecated() bool { return w != nil }
