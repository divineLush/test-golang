// Package native loads C/Rust shared libraries at runtime and exposes their
// exported functions as typed Go closures.
package native

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

typedef int64_t (*binary_fn)(int64_t, int64_t);

static int64_t call_binary(binary_fn f, int64_t a, int64_t b) {
    return f(a, b);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Op is a binary function exported by a shared library.
type Op func(a, b int64) int64

// Library is a handle to a dlopen'd shared library.
type Library struct {
	handle unsafe.Pointer
}

// Load opens the shared library at path.
func Load(path string) (*Library, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	handle := C.dlopen(cpath, C.RTLD_NOW)
	if handle == nil {
		return nil, fmt.Errorf("dlopen %s: %s", path, C.GoString(C.dlerror()))
	}
	return &Library{handle: handle}, nil
}

// Symbol resolves name to a binary Op.
func (l *Library) Symbol(name string) (Op, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	sym := C.dlsym(l.handle, cname)
	if sym == nil {
		return nil, fmt.Errorf("symbol %q not found", name)
	}

	fn := C.binary_fn(unsafe.Pointer(sym))
	return func(a, b int64) int64 {
		return int64(C.call_binary(fn, C.int64_t(a), C.int64_t(b)))
	}, nil
}
