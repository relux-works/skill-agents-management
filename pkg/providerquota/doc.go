// Package providerquota owns advisory subscription usage facts, their store,
// and optional harness Reader declarations. Quota never decides admission.
//
// A reader may run the harness and read its stdout; it never opens, stats or
// lists a file under a harness home; the harness is the only thing that reads
// its own credential. Here "run" means declaring a plan for the consumer:
// this module executes no quota plan. Parsers receive bytes and explicit read
// context, never an environment or a filesystem seam. Binary resolution uses
// the plugin's launch resolution; consumers must keep binary search roots out
// of harness homes. Identity homes are already normalized caller evidence;
// this package compares their path strings without resolving symlinks.
//
// The executor, throttle, cached query and spawn preflight belong to consumers.
// They close stdin on Complete, use an empty scratch cwd, and never refresh
// quota inside admission validation. Unsupported, absent and failed reads are
// different facts. Failed updates retain measurement ages, never headroom.
package providerquota
