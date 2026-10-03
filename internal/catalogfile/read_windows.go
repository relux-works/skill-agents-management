//go:build windows

package catalogfile

import "os"

// Local catalogs require the nonblocking regular-file contract. Hosted
// Windows launches retain their existing behavior; local catalog loading
// fails closed until a native equivalent is implemented and validated.
func ReadRegular(string, int64) ([]byte, error) { return nil, os.ErrInvalid }
