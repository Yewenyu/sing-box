//go:build !(darwin || linux)

package clash

import "os"

func getTunnelName(fd int32) (string, error) {
	return "", os.ErrInvalid
}
