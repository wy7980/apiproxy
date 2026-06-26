package switcher

import "fmt"

func errNotImplemented(feature string) error {
	return fmt.Errorf("not implemented: %s", feature)
}
