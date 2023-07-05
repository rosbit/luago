package lua

import (
	"sync"
	"os"
	"time"
)

type luaCtx struct {
	luavm *LuaContext
	mt   time.Time
}

var (
	luaCtxCache map[string]*luaCtx
	lock *sync.Mutex
)

func InitCache() {
	if lock != nil {
		return
	}
	lock = &sync.Mutex{}
	luaCtxCache = make(map[string]*luaCtx)
}

func LoadFileFromCache(path string, vars map[string]interface{}) (ctx *LuaContext, existing bool, err error) {
	lock.Lock()
	defer lock.Unlock()

	luaC, ok := luaCtxCache[path]

	if !ok {
		if ctx, err = createLuaContext(path, vars); err != nil {
			return
		}
		fi, _ := os.Stat(path)
		luaC = &luaCtx{
			luavm: ctx,
			mt: fi.ModTime(),
		}
		luaCtxCache[path] = luaC
		return
	}

	fi, e := os.Stat(path)
	if e != nil {
		err = e
		return
	}
	mt := fi.ModTime()
	if !luaC.mt.Equal(mt) {
		if ctx, err = createLuaContext(path, vars); err != nil {
			return
		}
		luaC.luavm = ctx
		luaC.mt = mt
	} else {
		existing = true
		ctx = luaC.luavm
	}
	return
}

func createLuaContext(path string, vars map[string]interface{}) (ctx *LuaContext, err error) {
	if ctx, err = NewContext(); err != nil {
		return
	}
	if err = ctx.LoadFile(path, vars); err != nil {
		return
	}
	return
}
