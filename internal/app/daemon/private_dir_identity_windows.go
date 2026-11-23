package daemon

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func privateDirectoryIdentity(file *os.File) (string, error) {
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	var native windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &native); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d:%d", native.VolumeSerialNumber, native.FileIndexHigh, native.FileIndexLow), nil
}
