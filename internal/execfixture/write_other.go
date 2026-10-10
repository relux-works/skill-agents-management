//go:build !linux

package execfixture

func lockExecutableWrite() func() { return func() {} }
