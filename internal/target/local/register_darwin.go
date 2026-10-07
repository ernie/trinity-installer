package local

/*
#cgo LDFLAGS: -framework CoreServices -framework CoreFoundation
#include <stdlib.h>
#include <CoreServices/CoreServices.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// lsRegister adds the bundle at path to the LaunchServices database, as Finder does for what it copies.
func lsRegister(path string) error {
	cs := C.CString(path)
	defer C.free(unsafe.Pointer(cs))
	url := C.CFURLCreateFromFileSystemRepresentation(C.kCFAllocatorDefault, (*C.UInt8)(unsafe.Pointer(cs)), C.CFIndex(len(path)), C.Boolean(1))
	if url == 0 {
		return fmt.Errorf("no file URL for %s", path)
	}
	defer C.CFRelease(C.CFTypeRef(url))
	if st := C.LSRegisterURL(url, C.Boolean(1)); st != 0 {
		return fmt.Errorf("LSRegisterURL: OSStatus %d", int(st))
	}
	return nil
}
