//go:build !windows

package vault

import "syscall"

const oNoFollow = syscall.O_NOFOLLOW
