package lua

// #include "lua.h"
// #include "lualib.h"
// #include "lauxlib.h"
// static void popN(lua_State *L, int n);
// static int getUpvalueIdx(int i);
// extern int go_obj_get(lua_State *ctx);
// extern int go_obj_set(lua_State *ctx);
// extern int go_obj_len(lua_State *ctx);
// extern int go_func_call(lua_State *ctx);
// extern int go_obj_free(lua_State *ctx);
import "C"
import (
	elutils "github.com/rosbit/go-embedding-utils"
	"reflect"
	"unsafe"
	"fmt"
	"strings"
)

func pushLuaMetaValue(ctx *C.lua_State, v interface{}) {
	if v == nil {
		C.lua_pushnil(ctx)
		return
	}

	vv := reflect.ValueOf(v)
	switch vv.Kind() {
	case reflect.Bool:
		if v.(bool) {
			C.lua_pushboolean(ctx, 1)
		} else {
			C.lua_pushboolean(ctx, 0)
		}
		return
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		C.lua_pushnumber(ctx, C.lua_Number(vv.Int()))
		return
	case reflect.Uint,reflect.Uint8,reflect.Uint16,reflect.Uint32,reflect.Uint64:
		C.lua_pushnumber(ctx, C.lua_Number(vv.Uint()))
		return
	case reflect.Float32, reflect.Float64:
		C.lua_pushnumber(ctx, C.lua_Number(vv.Float()))
		return
	case reflect.String:
		pushString(ctx, v.(string))
		return
	case reflect.Slice:
		t := vv.Type()
		if t.Elem().Kind() == reflect.Uint8 {
			pushString(ctx, string(v.([]byte)))
			return
		}
		fallthrough
	case reflect.Array, reflect.Map, reflect.Struct:
		pushValueWithMetatable(ctx, v, goObjMeta)
		return
	case reflect.Ptr:
		if vv.Elem().Kind() == reflect.Struct {
			pushValueWithMetatable(ctx, v, goObjMeta)
			return
		}
		pushLuaMetaValue(ctx, vv.Elem().Interface())
		return
	case reflect.Func:
		pushValueWithMetatable(ctx, v, goFuncMeta)
		return
	default:
		C.lua_pushnil(ctx)
		return
	}
}

func getArrayKey(ctx *C.lua_State, vv reflect.Value) (key int, err error) {
	// [ 1 ] ...
	// [ 2 ] key
	if C.lua_isinteger(ctx, 2) == 0 {
		err = fmt.Errorf("integer expected for array")
		return
	}
	key = int(C.lua_tointegerx(ctx, 2, (*C.int)(unsafe.Pointer(nil))))
	l := vv.Len()
	if key == 0 {
		err = fmt.Errorf("key out of range")
		return
	}
	if key < 0 {
		key = l + key + 1
	}
	if key < 1 || key > l {
		err = fmt.Errorf("key out of range")
		return
	}
	key -= 1 // go is 0-based
	return
}

func go_arr_get(ctx *C.lua_State, vv reflect.Value) C.int {
	// [1]: ...
	// [2]: key
	key, err := getArrayKey(ctx, vv)
	if err != nil {
		C.lua_pushnil(ctx)
		return 1
	}
	val := vv.Index(key)
	if !val.IsValid() || !val.CanInterface() {
		C.lua_pushnil(ctx)
		return 1
	}
	pushLuaMetaValue(ctx, val.Interface())
	return 1
}

func go_arr_set(ctx *C.lua_State, vv reflect.Value) C.int {
	// [1]: ...
	// [2]: key
	// [3]: val
	key, err := getArrayKey(ctx, vv)
	if err != nil {
		pushString(ctx, err.Error())
		C.lua_error(ctx)
		return 1
	}
	goVal, err := fromLuaValue(ctx)
	if err != nil {
		es := err.Error()
		pushString(ctx, es)
		C.lua_error(ctx)
		return 1
	}

	dest := vv.Index(key)
	if _, ok := goVal.(string); ok {
		goVal = fmt.Sprintf("%s", goVal) // deep copy
	}
	if err = elutils.SetValue(dest, goVal); err != nil {
		es := err.Error()
		pushString(ctx, es)
		C.lua_error(ctx)
		return 1
	}
	return 0
}

func go_map_get(ctx *C.lua_State, vv reflect.Value) C.int {
	// [1]: ...
	// [2]: key
	if C.lua_isstring(ctx, 2) == 0 {
		C.lua_pushnil(ctx)
		return 1
	}
	key := C.GoString(C.lua_tolstring(ctx, 2, (*C.ulong)(unsafe.Pointer(nil))))
	val := vv.MapIndex(reflect.ValueOf(key))
	if !val.IsValid() || !val.CanInterface() {
		C.lua_pushnil(ctx)
		return 1
	}
	pushLuaMetaValue(ctx, val.Interface())
	return 1
}

func go_map_set(ctx *C.lua_State, vv reflect.Value) C.int {
	// [1]: ...
	// [2]: key
	// [3]: val
	if C.lua_isstring(ctx, 2) == 0 {
		pushString(ctx, "string expected")
		C.lua_error(ctx)
		return 1
	}
	key := C.GoString(C.lua_tolstring(ctx, 2, (*C.ulong)(unsafe.Pointer(nil))))
	goVal, err := fromLuaValue(ctx)
	if err != nil {
		pushString(ctx, err.Error())
		C.lua_error(ctx)
		return 1
	}

	mapT := vv.Type()
	elType := mapT.Elem()
	dest := elutils.MakeValue(elType)
	if _, ok := goVal.(string); ok {
		goVal = fmt.Sprintf("%s", goVal) // deep copy
	}
	if err = elutils.SetValue(dest, goVal); err == nil {
		vv.SetMapIndex(reflect.ValueOf(key), dest)
		return 0
	} else {
		pushString(ctx, err.Error())
		C.lua_error(ctx)
		return 1
	}
}

func go_struct_get(ctx *C.lua_State, structVar reflect.Value) C.int {
	// [2]: ...
	// [2]: key
	if C.lua_isstring(ctx, 2) == 0 {
		C.lua_pushnil(ctx)
		return 1
	}
	key := C.GoString(C.lua_tolstring(ctx, 2, (*C.ulong)(unsafe.Pointer(nil))))
	var structE reflect.Value
	switch structVar.Kind() {
	case reflect.Struct:
		structE = structVar
	case reflect.Ptr:
		if structVar.Elem().Kind() != reflect.Struct {
			C.lua_pushnil(ctx)
			return 1
		}
		structE = structVar.Elem()
	default:
		C.lua_pushnil(ctx)
		return 1
	}
	name := upperFirst(key)
	fv := structE.FieldByName(name)
	if !fv.IsValid() {
		fv = structE.MethodByName(name)
		if !fv.IsValid() {
			if structE == structVar {
				C.lua_pushnil(ctx)
				return 1
			}
			fv = structVar.MethodByName(name)
			if !fv.IsValid() {
				C.lua_pushnil(ctx)
				return 1
			}
		}
		if fv.CanInterface() {
			pushValueWithMetatable(ctx, fv.Interface(), goFuncMeta)
			return 1
		}
		C.lua_pushnil(ctx)
		return 1
	}
	if !fv.CanInterface() {
		C.lua_pushnil(ctx)
		return 1
	}
	pushLuaMetaValue(ctx, fv.Interface())
	return 1
}

func go_struct_set(ctx *C.lua_State, vv reflect.Value) C.int {
	// [1]: ...
	// [2]: key
	// [3]: val
	if C.lua_isstring(ctx, 2) == 0 {
		pushString(ctx, "string expected")
		C.lua_error(ctx)
		return 1
	}
	key := C.GoString(C.lua_tolstring(ctx, 2, (*C.ulong)(unsafe.Pointer(nil))))
	goVal, err := fromLuaValue(ctx)
	if err != nil {
		pushString(ctx, err.Error())
		C.lua_error(ctx)
		return 1
	}

	var structE reflect.Value
	switch vv.Kind() {
	case reflect.Struct:
		structE = vv
	case reflect.Ptr:
		if vv.Elem().Kind() != reflect.Struct {
			pushString(ctx, "pointer of struct expected")
			C.lua_error(ctx)
			return 1
		}
		structE = vv.Elem()
	default:
		pushString(ctx, "unsupported type")
		C.lua_error(ctx)
		return 1
	}
	name := upperFirst(key)
	fv := structE.FieldByName(name)
	if !fv.IsValid() {
		pushString(ctx, "name not found")
		C.lua_error(ctx)
		return 1
	}
	if _, ok := goVal.(string); ok {
		goVal = fmt.Sprintf("%s", goVal) // deep copy
	}
	if err = elutils.SetValue(fv, goVal); err != nil {
		pushString(ctx, err.Error())
		C.lua_error(ctx)
		return 1
	}
	return 0
}

func getTargetIdx(ctx *C.lua_State, targetIdx C.int) (idx uint32) {
	p := (*uint32)(C.lua_topointer(ctx, targetIdx))
	idx = *p
	return
}

func getTargetValue(ctx *C.lua_State, targetIdx C.int) (v interface{}, ok bool) {
	// ....
	idx := getTargetIdx(ctx, targetIdx)

	ptr := getPtrStore(uintptr(unsafe.Pointer(ctx)))
	vPtr, o := ptr.lookup(idx)
	if !o {
		return
	}
	if vv, o := vPtr.(*interface{}); o {
		v = *vv
		ok = true
	}
	return
}

//export go_obj_get
func go_obj_get(ctx *C.lua_State) C.int {
	// [ 1 ]  go_meta_proxy
	// [ 2 ]  key
	v, ok := getTargetValue(ctx, 1)
	if !ok {
		C.lua_pushnil(ctx)
		return 1
	}
	if v == nil {
		C.lua_pushnil(ctx)
		return 1
	}
	switch vv := reflect.ValueOf(v); vv.Kind() {
	case reflect.Slice, reflect.Array:
		return go_arr_get(ctx, vv)
	case reflect.Map:
		return go_map_get(ctx, vv)
	case reflect.Struct, reflect.Ptr:
		return go_struct_get(ctx, vv)
	default:
		C.lua_pushnil(ctx)
		return 1
	}
}

//export go_obj_set
func go_obj_set(ctx *C.lua_State) C.int {
	// [ 1 ] go_meta_proxy
	// [ 2 ] key
	// [ 3 ] value
	v, ok := getTargetValue(ctx, 1)
	if !ok {
		pushString(ctx, "no target found")
		C.lua_error(ctx)
		return 1
	}
	if v == nil {
		pushString(ctx, "no value")
		C.lua_error(ctx)
		return 1
	}
	switch vv := reflect.ValueOf(v); vv.Kind() {
	case reflect.Slice, reflect.Array:
		return go_arr_set(ctx, vv)
	case reflect.Map:
		return go_map_set(ctx, vv)
	case reflect.Struct, reflect.Ptr:
		return go_struct_set(ctx, vv)
	default:
		pushString(ctx, "unsupport value type")
		C.lua_error(ctx)
		return 1
	}
}

//export go_obj_len
func go_obj_len(ctx *C.lua_State) C.int {
	// [ 1 ] go_meta_proxy
	v, ok := getTargetValue(ctx, 1)
	if !ok {
		C.lua_pushinteger(ctx, 0)
		return 1
	}
	if v == nil {
		C.lua_pushinteger(ctx, 0)
		return 1
	}
	switch vv := reflect.ValueOf(v); vv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		C.lua_pushinteger(ctx, C.lua_Integer(vv.Len()))
		return 1
	case reflect.Struct:
		structT := vv.Type()
		C.lua_pushinteger(ctx, C.lua_Integer(structT.NumField()))
		return 1
	case reflect.Ptr:
		if vv.Elem().Kind() != reflect.Struct {
			C.lua_pushinteger(ctx, 0)
			return 1
		}
		structT := vv.Elem().Type()
		C.lua_pushinteger(ctx, C.lua_Integer(structT.NumField()))
		return 1
	default:
		C.lua_pushinteger(ctx, 0)
		return 1
	}
}

//export go_func_call
func go_func_call(ctx *C.lua_State) C.int {
	// [ 1 ] go_meta_proxy
	// [ 2 - top ] args
	v, ok := getTargetValue(ctx, 1)
	if !ok {
		pushString(ctx, "not found")
		C.lua_error(ctx)
		return 1
	}
	if v == nil {
		pushString(ctx, "wrong type")
		C.lua_error(ctx)
		return 1
	}

	fnVal := reflect.ValueOf(v)
	if fnVal.Kind() != reflect.Func {
		pushString(ctx, "go function expected")
		C.lua_error(ctx)
		return 1
	}
	fnType := fnVal.Type()

	// make args for Golang function
	helper := elutils.NewGolangFuncHelperDirectly(fnVal, fnType)
	argc := int(C.lua_gettop(ctx)) - 1
	// [ arg1 arg2 ... argN ]
	getArgs := func(i int) interface{} {
		C.lua_pushnil(ctx)  // [ args ... null ] 
		C.lua_copy(ctx, C.int(i + 2), -1) // [ args ... argI ]  i is 0-based, lua is 1-based
		defer C.popN(ctx, 1) // [ args ... ]

		if goVal, err := fromLuaValue(ctx); err == nil {
			return goVal
		}
		return nil
	}
	res, e := helper.CallGolangFunc(argc, "lua-func", getArgs) // call Golang function

	// convert result (in var v) of Golang function to that of Lua.
	// 1. error
	if e != nil {
		es := e.Error()
		pushString(ctx, es)
		C.lua_error(ctx)
		return 1
	}

	// 2. no result
	if res == nil {
		return 0
	}

	// 3. array or scalar
	pushLuaMetaValue(ctx, res)
	return 1
}

//export go_obj_free
func go_obj_free(ctx *C.lua_State) C.int {
	// [ 1 ] go_meta_proxy
	// fmt.Printf("---go_obj_free called\n")
	idx := getTargetIdx(ctx, 1)
	ptr := getPtrStore(uintptr(unsafe.Pointer(ctx)))
	ptr.remove(idx)
	return 0
}

type metaMethod struct {
	name string
	method C.lua_CFunction
}
func registerMetatable(ctx *C.lua_State, metaName string, methods ...*metaMethod) {
	var name *C.char

	getStrPtr(&metaName, &name)
	C.luaL_newmetatable(ctx, name) // [ metatable ]

	for _, m := range methods {
		getStrPtr(&m.name, &name)
		C.lua_pushstring(ctx, name)   // [ metatable method-name ]
		C.lua_pushcclosure(ctx, m.method, 0) // [ metatable method-name method-func ]
		C.lua_rawset(ctx, -3) // [ metatable ] with metatable[method-name] = method-func
	}

	C.popN(ctx, 1) // [ ]
}

func registerGoMetatables(ctx *C.lua_State) {
	registerMetatable(ctx, goObjMeta, &metaMethod{
		name: __index, method: (C.lua_CFunction)(C.go_obj_get),
	}, &metaMethod{
		name: __newindex, method: (C.lua_CFunction)(C.go_obj_set),
	}, &metaMethod{
		name: __len, method: (C.lua_CFunction)(C.go_obj_len),
	}, &metaMethod{
		name: __gc, method: (C.lua_CFunction)(C.go_obj_free),
	})

	registerMetatable(ctx, goFuncMeta, &metaMethod{
		name: __call, method: (C.lua_CFunction)(C.go_func_call),
	}, &metaMethod{
		name: __gc, method: (C.lua_CFunction)(C.go_obj_free),
	})
}

func pushValueWithMetatable(ctx *C.lua_State, v interface{}, metaName string) {
	var name *C.char

	ptr := getPtrStore(uintptr(unsafe.Pointer(ctx)))
	idx := ptr.register(&v)

	p := (*uint32)(C.lua_newuserdatauv(ctx, 4, 0))   // [ userdata ]
	*p = idx

	getStrPtr(&metaName, &name)
	C.luaL_setmetatable(ctx, name) // [ userdata ] with metatable
}

func pushString(ctx *C.lua_State, s string) {
	var cstr *C.char
	var sLen C.int
	getStrPtrLen(&s, &cstr, &sLen)
	C.lua_pushlstring(ctx, cstr, C.size_t(sLen))
}

func upperFirst(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

