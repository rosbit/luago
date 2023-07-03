package lua

// #include "lua.h"
// static void popN(lua_State *L, int n);
import "C"
import (
	"reflect"
	"unsafe"
	"fmt"
	"strings"
)

func pushLuaValue(ctx *C.lua_State, v interface{}) {
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
	case reflect.Array:
		pushArr(ctx, vv)
		return
	case reflect.Map:
		pushObj(ctx, vv)
		return
	case reflect.Struct:
		pushStruct(ctx, vv)
		return
	case reflect.Ptr:
		if vv.Elem().Kind() == reflect.Struct {
			pushStruct(ctx, vv)
			return
		}
		pushLuaValue(ctx, vv.Elem().Interface())
		return
	case reflect.Func:
		if err := pushGoFunc(ctx, v); err != nil {
			C.lua_pushnil(ctx)
		}
		return
	default:
		// return fmt.Errorf("unsupported type %v", vv.Kind())
		C.lua_pushnil(ctx)
		return
	}
}

func fromLuaValue(ctx *C.lua_State) (goVal interface{}, err error) {
	var length C.size_t

	switch (C.lua_type(ctx, -1)) {
	case C.LUA_TNIL, C.LUA_TNONE:
		return
	case C.LUA_TBOOLEAN:
		goVal = C.lua_toboolean(ctx, -1) != 0
		return
	case C.LUA_TNUMBER:
		goVal = float64(C.lua_tonumberx(ctx, -1, (*C.int)(unsafe.Pointer(nil))))
		return
	case C.LUA_TSTRING:
		s := C.lua_tolstring(ctx, -1, &length)
		goVal = *(toString(s, int(length)))
		return
	case C.LUA_TTABLE:
		return fromLuaTable(ctx)
	// case C.LUA_TUSERDATA:
	// case LUA_TFUNCTION:
	// case LUA_TTHREAD:
	// case LUA_TLIGHTUSERDATA:
	default:
		err = fmt.Errorf("unsupporting type")
		return
	}
}

func fromLuaTable(ctx *C.lua_State) (goVal interface{}, err error) {
	// [ ... table ]
	res := make(map[string]interface{})
	arr := []interface{}{}
	i := 1
	maybeArr := true

	var val interface{}
	C.lua_pushnil(ctx) // [ ... table nil ]
	for C.lua_next(ctx, -2) != 0 {
		// [ ... table key val ]
		if maybeArr {
			if C.lua_isinteger(ctx, -2) != 0 {
				idx := int(C.lua_tointegerx(ctx, -2, (*C.int)(unsafe.Pointer(nil))))
				if idx != i {
					goto FOR_MAP
				}
				if val, err = fromLuaValue(ctx); err != nil {
					C.popN(ctx, 2) // [ ... table ]
					return
				}
				arr = append(arr, val)
				res[fmt.Sprintf("%d", idx)] = val // also save to map
				C.popN(ctx, 1) // [ ... table key ]
				i += 1
				continue
			}
		}
FOR_MAP:
		maybeArr = false
		// [ ... table key val ]
		if val, err = fromLuaValue(ctx); err != nil {
			C.popN(ctx, 2) // [ ... talbe ]
			return
		}
		C.popN(ctx, 1) // [ ... table key ]
		var key string
		switch C.lua_type(ctx, -1) {
		case C.LUA_TNUMBER:
			idx := int(C.lua_tointegerx(ctx, -1, (*C.int)(unsafe.Pointer(nil))))
			key = fmt.Sprintf("%d", idx)
		case C.LUA_TSTRING:
			var length C.size_t
			s := C.lua_tolstring(ctx, -1, &length)
			key = *(toString(s, int(length)))
		default:
			err = fmt.Errorf("key of string type expected")
			C.popN(ctx, 1) // [ ... table ]
			return
		}
		res[key] = val
	}
	if maybeArr {
		if len(arr) == 0 {
			return
		}
		goVal = arr
	} else {
		goVal = res
	}
	return
}

func pushString(ctx *C.lua_State, s string) {
	var cstr *C.char
	var sLen C.int
	getStrPtrLen(&s, &cstr, &sLen)
	C.lua_pushlstring(ctx, cstr, C.size_t(sLen))
}

func pushArr(ctx *C.lua_State, v reflect.Value) {
	if v.IsNil() {
		C.lua_createtable(ctx, 0, 0) // [ arr ]
		return
	}

	l := v.Len()
	C.lua_createtable(ctx, C.int(l), 0) // [ arr ]

	for i:=0; i<l; i++ {
		elm := v.Index(i).Interface()
		pushLuaValue(ctx, elm) // [ arr elm ]
		C.lua_rawseti(ctx, -2, C.lua_Integer(i+1)) // [ arr ] with arr[i+1] = elm
	}
}

func pushObj(ctx *C.lua_State, v reflect.Value) {
	if v.IsNil() {
		C.lua_createtable(ctx, 0, 0) // [ obj ]
		return
	}

	l := v.Len()
	C.lua_createtable(ctx, 0, C.int(l)) // [ obj ]

	mr := v.MapRange()
	for mr.Next() {
		k := mr.Key()
		v := mr.Value()

		pushLuaValue(ctx, k.Interface()) // [ obj k ]
		pushLuaValue(ctx, v.Interface()) // [ obj k v ]

		C.lua_rawset(ctx, -3) // [ obj ] with obj[k] = v
	}
}

// struct
func pushStruct(ctx *C.lua_State, structVar reflect.Value) {
	var structE reflect.Value
	if structVar.Kind() == reflect.Ptr {
		structE = structVar.Elem()
	} else {
		structE = structVar
	}
	structT := structE.Type()

	/*
	if structE == structVar {
		// struct is unaddressable, so make a copy of struct to an Elem of struct-pointer.
		// NOTE: changes of the copied struct cannot effect the original one. it is recommended to use the pointer of struct.
		structVar = reflect.New(structT) // make a struct pointer
		structVar.Elem().Set(structE)    // copy the old struct
		structE = structVar.Elem()       // structE is the copied struct
	}*/

	C.lua_createtable(ctx, 0, C.int(structT.NumField())) // [ obj ]
	for i:=0; i<structT.NumField(); i++ {
		name := structT.Field(i).Name
		fv := structE.FieldByName(name)

		if !fv.CanInterface() {
			continue
		}

		lName := lowerFirst(name)
		pushString(ctx, lName)            // [ obj lName ]
		pushLuaValue(ctx, fv.Interface()) // [ obj lName fv ]
		C.lua_rawset(ctx, -3) // [ obj ] with obj[lName] = fv
	}
}

func lowerFirst(name string) string {
	return strings.ToLower(name[:1]) + name[1:]
}
func upperFirst(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

