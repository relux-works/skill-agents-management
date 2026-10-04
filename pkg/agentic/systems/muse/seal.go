// Exec-guard seal export for the Muse plugin.
//
// Muse already declares a network-plan sealer in network_launch.go. That
// verifier has no exportable exec-guard projection; ExportSeal refuses it.
// ImportSeal refuses the unsealed guard because Muse declares a sealer.
// An exportable hosted Muse seal remains a separate Phase 2 contract.
package muse
