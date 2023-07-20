package lua

// #include "lua.h"
// #include "lualib.h"
// #include "lauxlib.h"
// static void popN(lua_State *L, int n);
// static int getUpvalueIdx(int i);
// static int pCall(lua_State *L, int nargs, int nresults) {
//	return lua_pcall(L, nargs, nresults, 0);
// }
// extern int go_obj_get(lua_State *ctx);
// extern int go_obj_set(lua_State *ctx);
// extern int go_obj_len(lua_State *ctx);
// extern int go_meta_proxy(lua_State *ctx);
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
		pusMetaGetterSetter(ctx, v)
		return
	case reflect.Ptr:
		if vv.Elem().Kind() == reflect.Struct {
			pusMetaGetterSetter(ctx, v)
			return
		}
		pushLuaMetaValue(ctx, vv.Elem().Interface())
		return
	case reflect.Func:
		pushGoFunc(ctx, v)
		return
	default:
		C.lua_pushnil(ctx)
		return
	}
}

func go_arr_get(ctx *C.lua_State, vv reflect.Value) C.int {
	// [1]: ...
	// [2]: key
	if C.lua_isinteger(ctx, 2) == 0 {
		C.lua_pushnil(ctx)
		return 1
	}
	key := int(C.lua_tointegerx(ctx, 2, (*C.int)(unsafe.Pointer(nil))))
	l := vv.Len()
	if key < 0 || key >= l {
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
	if C.lua_isinteger(ctx, 2) == 0 {
		pushString(ctx, "integer expected")
		C.lua_error(ctx)
		return 1
	}
	key := int(C.lua_tointegerx(ctx, 2, (*C.int)(unsafe.Pointer(nil))))
	goVal, err := fromLuaValue(ctx)
	if err != nil {
		es := err.Error()
		pushString(ctx, es)
		C.lua_error(ctx)
		return 1
	}

	l := vv.Len()
	if key < 0 || key >= l {
		pushString(ctx, "key out of range")
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
			pushGoFunc(ctx, fv.Interface())
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

func getTargetValue(ctx *C.lua_State, targetIdx C.int) (v interface{}, ok bool) {
	// ....
	C.lua_pushnil(ctx)             // [ ... nil ]
	C.lua_copy(ctx, targetIdx, -1) // [ ... go_meta_proxy ]
	C.pCall(ctx, 0, 1)             // [ ... idx ]
	idx := int(C.lua_tointegerx(ctx, -1, (*C.int)(unsafe.Pointer(nil))))
	C.popN(ctx, 1)                 // [ ... ]

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

//export go_meta_proxy
func go_meta_proxy(ctx *C.lua_State) C.int {
	idx := int(C.lua_tointegerx(ctx, C.getUpvalueIdx(1), (*C.int)(unsafe.Pointer(nil))))
	C.lua_pushinteger(ctx, C.lua_Integer(idx)) // [ idx ]
	return 1
}

func pusMetaGetterSetter(ctx *C.lua_State, v interface{}) {
	var name *C.char

	ptr := getPtrStore(uintptr(unsafe.Pointer(ctx)))
	idx := ptr.register(&v)

	C.lua_pushinteger(ctx, C.lua_Integer(idx)) // [ idx ]
	C.lua_pushcclosure(ctx, (C.lua_CFunction)(C.go_meta_proxy), 1) // [ go_meta_proxy ] with go_meta_proxy upvalue = idx
	getStrPtr(&goObjMeta, &name)
	C.luaL_setmetatable(ctx, name) // [ go_meta_proxy ] with goObjMeta as metatable
}

func getBoundTarget(ctx *C.lua_State) (targetV interface{}, isGoObj bool, err error) {
	// [ go_meta_proxy ]
	if C.lua_getmetatable(ctx, -1) == 0 {
		err = fmt.Errorf("not go_meta_proxy")
		return
	}
	// [ go_meta_proxy meta ]
	var name *C.char
	getStrPtr(&goObjMeta, &name)
	C.lua_getfield(ctx, C.LUA_REGISTRYINDEX, name) // luaL_getmetatable(ctx, name) // [ go_meta_proxy meta goObjMeta ]
	isGoObj = C.lua_rawequal(ctx, -2, -1) == 1
	C.popN(ctx, 2) // [ go_meta_proxy ]
	if !isGoObj {
		return
	}

	v, ok := getTargetValue(ctx, -2) // a trick to use -2
	if !ok {
		err = fmt.Errorf("target not found")
		return
	}
	targetV = v
	return
}

func registerGoObjMetatable(ctx *C.lua_State) {
	var name *C.char

	getStrPtr(&goObjMeta, &name)
	C.luaL_newmetatable(ctx, name) // [ metatable ]

	getStrPtr(&__index, &name)
	C.lua_pushstring(ctx, name)    // [ metatable __index ]
	C.lua_pushcclosure(ctx, (C.lua_CFunction)(C.go_obj_get), 0) // [ metatable __index getter ]
	C.lua_rawset(ctx, -3) // [ metatable ] with metatable[__index] = getter

	getStrPtr(&__newindex, &name)
	C.lua_pushstring(ctx, name)   // [ metatable __newindex ]
	C.lua_pushcclosure(ctx, (C.lua_CFunction)(C.go_obj_set), 0) // [ metatable __newindex setter ]
	C.lua_rawset(ctx, -3) // [ metatable ] with metatable[__newindex] = setter

	getStrPtr(&__len, &name)
	C.lua_pushstring(ctx, name)   // [ metatable __len]
	C.lua_pushcclosure(ctx, (C.lua_CFunction)(C.go_obj_len), 0) // [ metatable __newindex length ]
	C.lua_rawset(ctx, -3) // [ metatable ] with metatable[__len] = length

	C.popN(ctx, 1) // [ ]
}

func pushString(ctx *C.lua_State, s string) {
	var cstr *C.char
	var sLen C.int
	getStrPtrLen(&s, &cstr, &sLen)
	C.lua_pushlstring(ctx, cstr, C.size_t(sLen))
}

/*
func lowerFirst(name string) string {
	return strings.ToLower(name[:1]) + name[1:]
}*/
func upperFirst(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

