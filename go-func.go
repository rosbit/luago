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

	pushWrappedGoFunc(ctx)
	return
}

func getGoFuncValue(ctx *C.lua_State, funcName string) (funcPtr interface{}, err error) {
	luaCtx, e := getContext(ctx)
	if e != nil {
		err = e
		return
	}
	if len(luaCtx.env) == 0 {
		err = fmt.Errorf("no env found")
		return
	}
	fn, ok := luaCtx.env[funcName]
	if !ok {
		err = fmt.Errorf("func name %s not found", funcName)
		return
	}
	funcPtr = fn
	return
}

//export goFuncBridge
func goFuncBridge(ctx *C.lua_State) C.int {
	// get pointer of Golang function attached to goFuncBridge
	funcName := C.GoString(C.lua_tolstring(ctx, C.getUpvalueIdx(1), (*C.ulong)(unsafe.Pointer(nil))))
	fn, err := getGoFuncValue(ctx, funcName)
	if err != nil {
		pushString(ctx, err.Error())
		C.lua_error(ctx)
		return 1
	}
	fnVal := reflect.ValueOf(fn)
	if fnVal.Kind() != reflect.Func {
		pushString(ctx, fmt.Sprintf("related env with %s is not a go function", funcName))
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

func pushWrappedGoFunc(ctx *C.lua_State) {
	// [ ... funcName]
	C.lua_pushnil(ctx) // [ ... funcName nil ]
	C.lua_copy(ctx, -2, -1) // [ ... funcName funcName ]
	C.lua_pushcclosure(ctx, (C.lua_CFunction)(C.goFuncBridge), 1) // [ ... funcName goFuncBridge ] // with goFuncBridge upvalue = funcName
}

