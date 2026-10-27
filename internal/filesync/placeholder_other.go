//go:build !darwin

package filesync

import "os"

// IsPlaceholder reports an online-only file. Only macOS marks them in a way
// the scan can see without opening the file.
func IsPlaceholder(info os.FileInfo) bool { return false }
