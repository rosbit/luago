package lua

/*
#include "lua.h"
extern int goFuncBridge(lua_State *ctx);
static void popN(lua_State *L, int n);
static int getUpvalueIdx(int i) {
	return lua_upvalueindex(i);
}
*/
import "C"
import (
	elutils "github.com/rosbit/go-embedding-utils"
	"reflect"
	"unsafe"
	"fmt"
)

func pushGoFunc(ctx *C.lua_State, funcVar interface{}) (err error) {
	t := reflect.TypeOf(funcVar)
	if t.Kind() != reflect.Func {
		err = fmt.Errorf("funcVar expected to be a func")
		return
	}

	pushWrappedGoFunc(ctx, funcVar)
	return
}

func getPtrSotre(ctx *C.lua_State) (ptr *ptrStore) {
	ptr = ptrs.getPtrStore(uintptr(unsafe.Pointer(ctx)))
	return
}

//export goFuncBridge
func goFuncBridge(ctx *C.lua_State) C.int {
	// get pointer of Golang function attached to goFuncBridge
	idx := int(C.lua_tointegerx(ctx, C.getUpvalueIdx(1), (*C.int)(unsafe.Pointer(nil))))
	ptr := getPtrSotre(ctx)
	fnPtr, ok := ptr.lookup(idx)
	if !ok {
		pushString(ctx, "not found")
		C.lua_error(ctx)
		return 1
	}
	fnVarPtr, ok := fnPtr.(*interface{})
	if !ok {
		pushString(ctx, "wrong type")
		C.lua_error(ctx)
		return 1
	}
	fn := *fnVarPtr
	fnVal := reflect.ValueOf(fn)
	if fnVal.Kind() != reflect.Func {
		pushString(ctx, fmt.Sprintf("related env with idx %d is not a go function", idx))
		C.lua_error(ctx)
		return 1
	}
	fnType := fnVal.Type()

	// make args for Golang function
	helper := elutils.NewGolangFuncHelperDiretly(fnVal, fnType)
	argc := int(C.lua_gettop(ctx))
	// [ arg1 arg2 ... argN ]
	getArgs := func(i int) interface{} {
		C.lua_pushnil(ctx)  // [ args ... null ] 
		C.lua_copy(ctx, C.int(i - argc - 1), -1) // [ args ... argI ]
		defer C.popN(ctx, 1) // [ args ... ]

		if goVal, err := fromLuaValue(ctx); err == nil {
			return goVal
		}
		return nil
	}
	v, e := helper.CallGolangFunc(argc, "lua-func", getArgs) // call Golang function

	// convert result (in var v) of Golang function to that of Lua.
	// 1. error
	if e != nil {
		es := e.Error()
		pushString(ctx, es)
		C.lua_error(ctx)
		return 1
	}

	// 2. no result
	if v == nil {
		return 0
	}

	// 3. array or scalar
	pushLuaValue(ctx, v)
	return 1
}

func pushWrappedGoFunc(ctx *C.lua_State, fnVar interface{}) {
	ptr := getPtrSotre(ctx)
	idx := ptr.register(&fnVar)

	// [ ... funcName]
	C.lua_pushinteger(ctx, C.lua_Integer(idx)) // [ ... funcName idx ]
	C.lua_pushcclosure(ctx, (C.lua_CFunction)(C.goFuncBridge), 1) // [ ... funcName goFuncBridge ] // with goFuncBridge upvalue = idx
}

