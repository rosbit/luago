package lua

// #include "lua.h"
// #include "lauxlib.h"
// static void popN(lua_State *L, int n);
// static void pushGlobal(lua_State *L);
// // static int pCall(lua_State *L, int nargs, int nresults);
// static int pCall(lua_State *L, int nargs, int nresults) {
//	return lua_pcall(L, nargs, nresults, 0);
// }
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

		return callLuaFuncFromGo(ctx, helper, args)
	}
}

func collectFuncResult(ctx *C.lua_State, nOut int) (goVal interface{}, err error) {
	// [ some-obj o1 o2 .. oN ]
	switch nOut {
	case 0:
	case 1:
		goVal, err = fromLuaValue(ctx)
	default:
		res := make([]interface{}, nOut)
		for i:=0; i<nOut; i++ {
			C.lua_pushnil(ctx) // [ some-obj o1 o2 .. oN nil ]
			C.lua_copy(ctx, C.int(i - nOut - 1), -1) // [ some-obj o1 o2 .. oN oI]
			res[i], err = fromLuaValue(ctx)
			C.popN(ctx, 1) // [ some-obj o1 o2 .. oN ]
			if err != nil {
				break
			}
		}
		goVal = res
	}
	C.popN(ctx, C.int(nOut+1)) // []
	return
}

// called by wrapFunc() and fromLuaFunc::bindGoFunc
func callLuaFuncFromGo(ctx *C.lua_State, helper *elutils.EmbeddingFuncHelper, args []reflect.Value)  (results []reflect.Value) {
	// [ some-obj function ]

	// push Lua args
	argc := 0
	itArgs := helper.MakeGoFuncArgs(args)
	for arg := range itArgs {
		pushLuaMetaValue(ctx, arg)
		argc += 1
	}
	// [ some-obj function arg1 arg2 ... argN ]

	// call Lua function
	nOut, withLastErr := helper.NumOut()
	if withLastErr {
		nOut -= 1
	}
	var err error
	var goVal interface{}
	if C.pCall(ctx, C.int(argc), C.int(nOut)) != 0 {
		// [ some-obj err ]
		err = fmt.Errorf("%s", C.GoString(C.lua_tolstring(ctx, -1, (*C.ulong)(unsafe.Pointer(nil)))))
		C.popN(ctx, 2) // [ ]
		goto OUT
	}

	// [ some-obj o1 o2 .. oN ]
	/*
	switch nOut {
	case 0:
	case 1:
		goVal, err = fromLuaValue(ctx)
	default:
		res := make([]interface{}, nOut)
		for i:=0; i<nOut; i++ {
			C.lua_pushnil(ctx) // [ some-obj o1 o2 .. oN nil ]
			C.lua_copy(ctx, C.int(i - nOut - 1), -1) // [ some-obj o1 o2 .. oN oI]
			res[i], err = fromLuaValue(ctx)
			C.popN(ctx, 1) // [ some-obj o1 o2 .. oN ]
			if err != nil {
				break
			}
		}
		goVal = res
	}
	C.popN(ctx, C.int(nOut+1)) // []
	*/
	goVal, err = collectFuncResult(ctx, nOut)

OUT:
	// convert result to golang
	results = helper.ToGolangResults(goVal, nOut > 1, err)
	return
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
	/*
	switch nOut {
	case 0:
	case 1:
		res, err = fromLuaValue(ctx)
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
	}
	C.popN(ctx, C.int(nOut+1)) // [ ]
	*/
	res, err = collectFuncResult(ctx, nOut)
	return
}

// called by value.go::fromLuaValue()
func fromLuaFunc(ctx *C.lua_State) (bindGoFunc elutils.FnBindGoFunc) {
	// [ function ]
	C.lua_pushnil(ctx) // [ funciton nil ]
	C.lua_copy(ctx, -2, -1) // [ function function-duplicated ]
	idx := C.luaL_ref(ctx, C.LUA_REGISTRYINDEX) // [ function ] with registry[idx] = function

	bindGoFunc = func(fnVarPtr interface{}) elutils.FnGoFunc {
		helper, e := elutils.NewEmbeddingFuncHelper(fnVarPtr)
		if e != nil {
			return nil
		}

		return func(args []reflect.Value) (results []reflect.Value) {
			// reload the function when calling go-function
			C.lua_pushnil(ctx) // [ nil ] used as a placeholder
			C.lua_rawgeti(ctx, C.LUA_REGISTRYINDEX, C.lua_Integer(idx)) // [ nil function ]

			return callLuaFuncFromGo(ctx, helper, args)
		}
	}

	return bindGoFunc
}
