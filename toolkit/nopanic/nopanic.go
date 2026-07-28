package nopanic

import (
	"github.com/sqlbunny/errors"
)

func Run(fn func() error) (err error) {
	// This very convoluted code is because there's no way to distinguish
	// between `panic(nil)` and no panic with just `recover()` (both return nil)
	// https://github.com/golang/go/issues/25448
	panicked := true
	err = nil
	defer func() {
		if panicked {
			rvr := recover()
			if rvre, ok := rvr.(error); ok {
				err = errors.Errorf("panic: %w", rvre)
			} else {
				err = errors.Errorf("panic: %+v", rvr)
			}
		}
	}()

	err = fn()

	panicked = false
	return
}
