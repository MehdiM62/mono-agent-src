//go:build windows

package account

import (
	"fmt"
	"syscall"
)

// folderID is dir's volume serial number and file index, "" when the system does not say. A
// rename on one volume keeps them; a copy, or a move to another volume, gets new ones.
func folderID(dir string) string {
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return ""
	}
	h, err := syscall.CreateFile(p, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow)
}
