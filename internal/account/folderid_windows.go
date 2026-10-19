//go:build windows

package account

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fileIDInfo is FILE_ID_INFO: the 64-bit volume serial number and the 128-bit file id (ReFS ids
// are 128-bit; GetFileInformationByHandle's 32-bit serial and 64-bit index are not enough).
type fileIDInfo struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

// folderID is dir's volume serial number and file id, "" when the system does not say (a file
// system without file ids). A rename on one volume keeps them; a copy, or a move to another
// volume, gets new ones. os.Open handles long paths and opens a folder for its handle.
func folderID(dir string) string {
	f, err := os.Open(dir)
	if err != nil {
		return ""
	}
	defer f.Close()
	var info fileIDInfo
	if err := windows.GetFileInformationByHandleEx(windows.Handle(f.Fd()), windows.FileIdInfo,
		(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return ""
	}
	return volumeFileID(info.VolumeSerialNumber, info.FileID)
}
