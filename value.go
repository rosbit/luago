package lua

// #include "lua.h"
// static void popN(lua_State *L, int n);
import "C"
import (
	"reflect"
	"unsafe"
	"fmt"
)

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
		// goVal = C.GoStringN(s, C.int(length))
		return
	case C.LUA_TTABLE:
		return fromLuaTable(ctx)
	// case C.LUA_TFUNCTION:
	case C.LUA_TUSERDATA:
		targetV, ok := getTargetValue(ctx, -1)
		if !ok {
			err = fmt.Errorf("target not found")
			return
		}
		switch vv := reflect.ValueOf(targetV); vv.Kind() {
		case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
			goVal = targetV
			return
		case reflect.Ptr:
			goVal = vv.Elem().Interface()
			return
		default:
			err = fmt.Errorf("unknown type")
			return
		}
	// case LUA_TTHREAD:
	case C.LUA_TLIGHTUSERDATA:
		goVal = (unsafe.Pointer)(C.lua_touserdata(ctx, -1))
		return
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
				if _, ok := val.(string); ok {
					fmt.Sprintf("%s", val) // deep copy
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
		if _, ok := val.(string); ok {
			val = fmt.Sprintf("%s", val) // deep copy
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
			// key = C.GoStringN(s, C.int(length))
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

