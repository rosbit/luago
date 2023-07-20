package lua

// #include "lua.h"
// static void popN(lua_State *L, int n);
// static void pushGlobal(lua_State *L);
// static int pCall(lua_State *L, int nargs, int nresults);
import "C"
import (
	elutils "github.com/rosbit/go-embedding-utils"
	"reflect"
	"unsafe"
	"fmt"
)

func bindFunc(ctx *C.lua_State, funcName string, funcVarPtr interface{}) (err error) {
	helper, e := elutils.NewEmbeddingFuncHelper(funcVarPtr)
	if e != nil {
		err = e
		return
	}
	helper.BindEmbeddingFunc(wrapFunc(ctx, funcName, helper))
	return
}

func wrapFunc(ctx *C.lua_State, funcName string, helper *elutils.EmbeddingFuncHelper) elutils.FnGoFunc {
	return func(args []reflect.Value) (results []reflect.Value) {
		// reload the function when calling go-function
		C.pushGlobal(ctx) // [ global ]
		getVar(ctx, funcName) // [ global function ]

		// push Lua args
		argc := 0
		itArgs := helper.MakeGoFuncArgs(args)
		for arg := range itArgs {
			pushLuaMetaValue(ctx, arg)
			argc += 1
		}
		// [ global function arg1 arg2 ... argN ]

		// call Lua function
		nOut, withLastErr := helper.NumOut()
		if withLastErr {
			nOut -= 1
		}
		var err error
		var goVal interface{}
		if C.pCall(ctx, C.int(argc), C.int(nOut)) != 0 {
			// [ global err ]
			err = fmt.Errorf("%s", C.GoString(C.lua_tolstring(ctx, -1, (*C.ulong)(unsafe.Pointer(nil)))))
			C.popN(ctx, 2) // [ ]
			goto OUT
		}

		// [ global o1 o2 .. oN ]
		if nOut == 1 {
			goVal, err = fromLuaValue(ctx)
		} else {
			res := make([]interface{}, nOut)
			for i:=0; i<nOut; i++ {
				C.lua_pushnil(ctx) // [ global o1 o2 .. oN nil ]
				C.lua_copy(ctx, C.int(i - nOut - 1), -1) // [ global o1 o2 .. oN oI]
				res[i], err = fromLuaValue(ctx)
				C.popN(ctx, 1) // [ global o1 o2 .. oN ]
				if err != nil {
					break
				}
			}
			goVal = res
		}
		C.popN(ctx, C.int(nOut+1)) // []

OUT:
		// convert result to golang
		results = helper.ToGolangResults(goVal, nOut > 1, err)
		return
	}
}

func callFunc(ctx *C.lua_State, args ...interface{}) (res interface{}, err error) {
	// [ obj function ]
	objIdx := int(C.lua_gettop(ctx) - 1)

	n := len(args)
	for _, arg := range args {
		pushLuaMetaValue(ctx, arg)
	}
	// [ obj function arg1 arg2 ... argN ]

	if C.pCall(ctx, C.int(n), C.LUA_MULTRET) != 0 {
		// [ obj err ]
		err = fmt.Errorf("%s", C.GoString(C.lua_tolstring(ctx, -1, (*C.ulong)(unsafe.Pointer(nil)))))
		C.popN(ctx, 2) // [ ]
		return
	}

	// [ obj o1 o2 ... oN ]
	topIdx := int(C.lua_gettop(ctx))
	nOut := topIdx - objIdx
	switch nOut {
	case 0:
	case 1:
		res, err = fromLuaValue(ctx)
		C.popN(ctx, 2) // [ ]
	default:
		arr := make([]interface{}, nOut)
		for i:=0; i<nOut; i++ {
			C.lua_pushnil(ctx) // [ obj o1 o2 .. oN nil ]
			C.lua_copy(ctx, C.int(i - nOut - 1), -1) // [ obj o1 o2 .. oN oI]
			arr[i], err = fromLuaValue(ctx)
			C.popN(ctx, 1) // [ obj o1 o2 .. oN ]
			if err != nil {
				break
			}
		}
		res = arr
		C.popN(ctx, C.int(nOut+1)) // [ ]
	}
	return
}
