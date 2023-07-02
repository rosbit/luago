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

	pushWrappedGoFunc(ctx, funcVar, t)
	return
}

//export goFuncBridge
func goFuncBridge(ctx *C.lua_State) C.int {
	// get pointer of Golang function attached to goFuncBridge
	ptr := C.lua_topointer(ctx, C.getUpvalueIdx(1))
	fn := *((*interface{})(unsafe.Pointer(ptr)))
	fnVal := reflect.ValueOf(fn)
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
		var cstr *C.char
		var length C.int
		getStrPtrLen(&es, &cstr, &length)
		C.lua_pushlstring(ctx, cstr, C.size_t(length))
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

func pushWrappedGoFunc(ctx *C.lua_State, fnVar interface{}, fnType reflect.Type) {
	fnVarPtr := &fnVar

	// [ ... ]
	C.lua_pushlightuserdata(ctx, unsafe.Pointer(fnVarPtr)) // [ ... fnVarPtr ]
	C.lua_pushcclosure(ctx, (C.lua_CFunction)(C.goFuncBridge), 1) // [ ... goFuncBridge ] // with goFuncBridge upvalue = fnVarPtr
}

