//go:build !windows

package build

import "fmt"

func startUTF8CacheServer(string) error {
	return fmt.Errorf("sccache の server の起こし直しは Windows でだけ動きます")
}
