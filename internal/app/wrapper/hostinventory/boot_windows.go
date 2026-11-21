//go:build windows

package hostinventory

import (
	"encoding/hex"
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

func BootIdentity() (string, error) {
	var information [32]byte
	var length uint32
	status, _, _ := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtQuerySystemInformation").Call(90, uintptr(unsafe.Pointer(&information[0])), uintptr(len(information)), uintptr(unsafe.Pointer(&length)))
	if int32(status) < 0 {
		return "", fmt.Errorf("query Windows boot identity: NTSTATUS %x", status)
	}
	return hex.EncodeToString(information[:16]), nil
}
