//go:build windows

package futu

import (
	"errors"
	"os"
)

// Windows has no flock(2): cross-process rate limiting degrades to the pure
// in-memory limiter there (timestamp files are still opened but never locked,
// so no cross-process rhythm is enforced — see crossProcessNext degrade path).
var errNoFlock = errors.New("futu: flock unsupported on this platform")

func flockExclusive(*os.File) error { return errNoFlock }
func flockRelease(*os.File) error   { return errNoFlock }
