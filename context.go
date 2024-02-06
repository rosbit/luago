package lua

/*
#include <stdlib.h>
#include "lua.h"
#include "lauxlib.h"
#include "lualib.h"
static int doString(lua_State *L, const char *str) {
	return luaL_dostring(L, str);
}
static int doFile(lua_State *L, const char *filename) {
	return luaL_dofile(L, filename);
}
static void popN(lua_State *L, int n) {
	lua_pop(L, n);
}
static void pushGlobal(lua_State *L) {
	lua_pushglobaltable(L);
}
*/
import "C"
import (
	"reflect"
	"unsafe"
	"fmt"
	"sync"
	"runtime"
)

type LuaContext struct {
	c *C.lua_State
	mu *sync.Mutex
}

func NewContext() (*LuaContext, error) {
	ctx := C.luaL_newstate()
	if ctx == (*C.lua_State)(unsafe.Pointer(nil)) {
		return nil, fmt.Errorf("failed to create context")
	}
	loadPreludeModules(ctx)
	c := &LuaContext {
		c: ctx,
		mu: &sync.Mutex{},
	}
	runtime.SetFinalizer(c, freeLuaContext)
	return c, nil
}

func freeLuaContext(ctx *LuaContext) {
	c := ctx.c
	C.lua_close(c)
	delPtrStore((uintptr(unsafe.Pointer(c))))
	// fmt.Printf("context freed\n")
}

func loadPreludeModules(ctx *C.lua_State) {
	C.luaL_openlibs(ctx)
	registerGoMetatables(ctx)
}

func (ctx *LuaContext) LoadScript(script string, env map[string]interface{}) (err error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	c := ctx.c
	setEnv(c, env)

	cstr := C.CString(script)
	defer C.free(unsafe.Pointer(cstr))

	if C.doString(c, cstr) == 0 {
		return
	}
	err = fmt.Errorf("failed to loadScript: %s", C.GoString(C.lua_tolstring(c, -1, (*C.ulong)(unsafe.Pointer(nil)))))
	return
}

func (ctx *LuaContext) LoadFile(scriptFile string, env map[string]interface{}) (err error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	c := ctx.c
	setEnv(c, env)

	cstr := C.CString(scriptFile)
	defer C.free(unsafe.Pointer(cstr))

	if C.doFile(c, cstr) == 0 {
		return
	}

	err = fmt.Errorf("failed to loadFile: %s", C.GoString(C.lua_tolstring(c, -1, (*C.ulong)(unsafe.Pointer(nil)))))
	return
}

func setEnv(ctx *C.lua_State, env map[string]interface{}) {
	C.pushGlobal(ctx) // [ global ]
	defer C.popN(ctx, 1) // [ ]

	for k, _ := range env {
		v := env[k]
		pushString(ctx, k)    // [ global k ]
		pushLuaMetaValue(ctx, v)  // [ global k v ]
		C.lua_rawset(ctx, -3) // [ global ] with global[k] = v
	}
}

func getVar(ctx *C.lua_State, name string) (exsiting bool) {
	// [ obj ]
	var cstr *C.char
	var nameLen C.int
	getStrPtrLen(&name, &cstr, &nameLen)
	C.lua_pushlstring(ctx, cstr, C.size_t(nameLen)) // [ obj name ]
	C.lua_rawget(ctx, -2) // [ obj result ]
	return C.lua_type(ctx, -1) != C.LUA_TNIL
}

func (ctx *LuaContext) GetGlobal(name string) (res interface{}, err error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	c := ctx.c
	C.pushGlobal(c) // [ global ]
	defer C.popN(c, 2) // [ ]

	if !getVar(c, name) { // [ global result ]
		err = fmt.Errorf("global %s not found", name)
		return
	}
	return fromLuaValue(c)
}

func (ctx *LuaContext) CallFunc(funcName string, args ...interface{}) (res interface{}, err error) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	c := ctx.c
	C.pushGlobal(c) // [ global ]

	if !getVar(c, funcName) { // [ global funcName-result ]
		err = fmt.Errorf("function %s not found", funcName)
		C.popN(c, 2) // [ ]
		return
	}

	if C.lua_type(c, -1) != C.LUA_TFUNCTION {
		err = fmt.Errorf("var %s is not with type function", funcName)
		C.popN(c, 2) // [ ]
		return
	}

	// [ global function ]
	return callFunc(c, args...)
}

// bind a var of golang func with a JS function name, so calling JS function
// is just calling the related golang func.
// @param funcVarPtr  in format `var funcVar func(....) ...; funcVarPtr = &funcVar`
func (ctx *LuaContext) BindFunc(funcName string, funcVarPtr interface{}) (err error) {
	if funcVarPtr == nil {
		err = fmt.Errorf("funcVarPtr must be a non-nil poiter of func")
		return
	}
	t := reflect.TypeOf(funcVarPtr)
	if t.Kind() != reflect.Ptr || t.Elem().Kind() != reflect.Func {
		err = fmt.Errorf("funcVarPtr expected to be a pointer of func")
		return
	}

	ctx.mu.Lock()
	defer ctx.mu.Unlock()

	c := ctx.c

	C.pushGlobal(c) // [ global ]
	if !getVar(c, funcName) { // [ global funcName-result ]
		err = fmt.Errorf("function %s not found", funcName)
		C.popN(c, 2) // [ ]
		return
	}

	if C.lua_type(c, -1) != C.LUA_TFUNCTION {
		err = fmt.Errorf("var %s is not with type function", funcName)
		C.popN(c, 2) // [ ]
		return
	}

	C.popN(c, 2) // [ ] function will be restored when calling
	return bindFunc(ctx, funcName, funcVarPtr)
}

func (ctx *LuaContext) BindFuncs(funcName2FuncVarPtr map[string]interface{}) (err error) {
	for funcName, funcVarPtr := range funcName2FuncVarPtr {
		if err = ctx.BindFunc(funcName, funcVarPtr); err != nil {
			return
		}
	}
	return
}
