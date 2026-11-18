package main

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ok               = 0
	noInterface      = 0x80004002
	badPointer       = 0x80004003
	failed           = 0x80004005
	noAggregation    = 0x80040110
	classUnavailable = 0x80040111
)

func guid(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic(err)
	}
	return g
}

var (
	clsid         = guid("{7F033F4B-8C89-4586-9E26-A910C3CB83CC}")
	profile       = guid("{658A3318-9146-4D6A-93A8-54794E64C849}")
	unknown       = guid("{00000000-0000-0000-C000-000000000046}")
	classFactory  = guid("{00000001-0000-0000-C000-000000000046}")
	textProcessor = guid("{AA80E7F7-2021-11D2-93E0-0060B067B86E}")
	keySink       = guid("{AA80E7F5-2021-11D2-93E0-0060B067B86E}")
	keyManager    = guid("{AA80E7F0-2021-11D2-93E0-0060B067B86E}")
	threadSink    = guid("{AA80E80E-2021-11D2-93E0-0060B067B86E}")
	sourceIID     = guid("{4EA48A35-60AE-446F-8FD6-E6A8D82459F7}")
	editIID       = guid("{AA80E803-2021-11D2-93E0-0060B067B86E}")
	insertIID     = guid("{55CE16BA-3014-41C1-9CEB-FADE1446AC6C}")
)

var ole = windows.NewLazySystemDLL("ole32.dll")
var alloc = ole.NewProc("CoTaskMemAlloc")
var free = ole.NewProc("CoTaskMemFree")

// COM sees only native memory. Go objects are rooted in this map and the
// native interface stores an integer ID, never a Go pointer.
type face struct{ table, id uintptr }
type object struct {
	refs    uintptr
	primary uintptr
	faces   map[windows.GUID]uintptr
	service *service
	edit    *editRequest
}

var objects = struct {
	sync.Mutex
	next  uintptr
	items map[uintptr]*object
}{items: make(map[uintptr]*object)}

func lookup(this uintptr) *object {
	objects.Lock()
	defer objects.Unlock()
	return objects.items[(*face)(unsafe.Pointer(this)).id]
}

func create(ids []windows.GUID, tables []uintptr, s *service, edit *editRequest) *object {
	objects.Lock()
	defer objects.Unlock()
	objects.next++
	o := &object{refs: 1, faces: make(map[windows.GUID]uintptr), service: s, edit: edit}
	for i, id := range ids {
		p, _, _ := alloc.Call(unsafe.Sizeof(face{}))
		if p == 0 {
			for _, ptr := range o.faces {
				free.Call(ptr)
			}
			return nil
		}
		*(*face)(unsafe.Pointer(p)) = face{tables[i], objects.next}
		o.faces[id] = p
		if i == 0 {
			o.primary = p
		}
	}
	objects.items[objects.next] = o
	return o
}

func addRef(this uintptr) uintptr {
	objects.Lock()
	defer objects.Unlock()
	o := objects.items[(*face)(unsafe.Pointer(this)).id]
	o.refs++
	return o.refs
}

func release(this uintptr) uintptr {
	objects.Lock()
	id := (*face)(unsafe.Pointer(this)).id
	o := objects.items[id]
	o.refs--
	count := o.refs
	if count == 0 {
		delete(objects.items, id)
	}
	objects.Unlock()
	if count == 0 {
		for _, ptr := range o.faces {
			free.Call(ptr)
		}
	}
	return count
}

func query(this, iid, out uintptr) uintptr {
	if iid == 0 || out == 0 {
		return badPointer
	}
	p := (*uintptr)(unsafe.Pointer(out))
	*p = 0
	id := *(*windows.GUID)(unsafe.Pointer(iid))
	o := lookup(this)
	result := o.faces[id]
	if id == unknown {
		result = o.primary
	}
	if result == 0 {
		return noInterface
	}
	addRef(this)
	*p = result
	return ok
}

func method(this uintptr, slot int, args ...any) uintptr {
	table := *(*uintptr)(unsafe.Pointer(this))
	fn := *(*uintptr)(unsafe.Pointer(table + uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	var pinner runtime.Pinner
	defer pinner.Unpin()
	words := make([]uintptr, 1, len(args)+1)
	words[0] = this
	for _, arg := range args {
		switch v := arg.(type) {
		case unsafe.Pointer:
			pinner.Pin(v)
			words = append(words, uintptr(v))
		case uintptr:
			words = append(words, v)
		case int:
			words = append(words, uintptr(v))
		default:
			panic("unsupported COM argument")
		}
	}
	result, _, _ := syscall.SyscallN(fn, words...)
	return result
}

func getInterface(this uintptr, id *windows.GUID) uintptr {
	var result uintptr
	if hr := method(this, 0, ptr(id), ptr(&result)); isFailure(hr) {
		return 0
	}
	return result
}

func ptr[T any](value *T) unsafe.Pointer { return unsafe.Pointer(value) }
func isFailure(hr uintptr) bool          { return int32(hr) < 0 }

func newTable(callbacks ...any) uintptr {
	count := 3 + len(callbacks)
	p, _, _ := alloc.Call(uintptr(count) * unsafe.Sizeof(uintptr(0)))
	if p == 0 {
		panic("could not allocate COM vtable")
	}
	table := unsafe.Slice((*uintptr)(unsafe.Pointer(p)), count)
	for i, fn := range append([]any{query, addRef, release}, callbacks...) {
		table[i] = syscall.NewCallback(fn)
	}
	return p
}

func makeService(_, outer, iid, out uintptr) uintptr {
	if out == 0 {
		return badPointer
	}
	*(*uintptr)(unsafe.Pointer(out)) = 0
	if outer != 0 {
		return noAggregation
	}
	o := create([]windows.GUID{textProcessor, keySink, threadSink}, []uintptr{processorTable, keysTable, threadTable}, &service{}, nil)
	if o == nil {
		return failed
	}
	defer release(o.primary)
	return query(o.primary, iid, out)
}

var factoryTable, processorTable, keysTable, threadTable, editTable uintptr

func init() {
	factoryTable = newTable(makeService, func(_, _ uintptr) uintptr { return ok })
	processorTable = newTable(activate, deactivate)
	keysTable = newTable(onForeground, testDown, testUp, keyDown, keyUp, preservedKey)
	threadTable = newTable(ignoreDocument, ignoreDocument, focusDocument, ignoreDocument, focusContextLost)
	editTable = newTable(doEdit)
}
