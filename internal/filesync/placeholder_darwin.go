package filesync

import (
	"os"
	"syscall"
)

// sfDataless is the BSD file flag macOS sets on a file whose content is only
// in the cloud (SF_DATALESS). Reading it would download it, so the scan only
// looks at the flag.
const sfDataless = 0x40000000

// IsPlaceholder reports an online-only file.
func IsPlaceholder(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Flags&sfDataless != 0
}
