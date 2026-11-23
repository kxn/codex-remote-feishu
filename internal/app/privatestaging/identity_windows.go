package privatestaging

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
)

func identity(f *os.File) (string, error) {
	var i windows.ByHandleFileInformation
	if e := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &i); e != nil {
		return "", e
	}
	return fmt.Sprintf("%d:%d:%d", i.VolumeSerialNumber, i.FileIndexHigh, i.FileIndexLow), nil
}

// Windows access control is inherited from the private daemon state directory.
func privateDirectory(info os.FileInfo) bool { return info.IsDir() }
