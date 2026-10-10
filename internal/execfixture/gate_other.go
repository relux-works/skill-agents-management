//go:build !unix

package execfixture

import "fmt"

func makeFIFO(path string) error { return fmt.Errorf("fixture FIFO requires Unix: %s", path) }
