//go:build darwin

package framework

import "syscall"

// freeDiskBytes returns the bytes available to an unprivileged user at path.
// On darwin Statfs_t.Bavail is already uint64.
func freeDiskBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
