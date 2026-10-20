//go:build windows

package secrets

func ownedHome(dir string, uid int, parent string) bool { return false }
