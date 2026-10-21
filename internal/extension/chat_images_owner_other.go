//go:build !unix

package extension

import "os"

// ownedByMe: without owner ids, the per-user temp folder is the boundary.
func ownedByMe(os.FileInfo) bool { return true }
