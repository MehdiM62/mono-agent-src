//go:build !linux

package aitask

// ProcessWithin is not checked off Linux (there is no portable parent walk);
// the token alone then binds the holder.
var ProcessWithin = func(pid int) bool { return true }
