# luago, makes lua be embedded easily

[Lua](http://www.lua.org/) is a powerful, efficient, lightweight, embeddable scripting language.
For most of developers who are not familiar with C language, it is very tedious to call lua\_pushxxx() and
lua\_pop() to make use of the power of Lua. Though there are some binding
implementations of Lua for languages other than C, most of them inherit the
methods of using API of Lua.

`luago` is a package wrapping Lua and making it a **pragmatic embeddable** language.
With some helper functions provided by `luago`, calling Golang functions from Lua, 
or calling Lua functions from Golang are both very simple. So, with the help of `luago`, Lua
can be embedded in Golang application easily.

### Install

The package is fully go-getable, So, just type

  `go get github.com/rosbit/luago`

to install.

### Usage

#### 1. Evaluates expressions

```go
package main

import (
  lua "github.com/rosbit/luago"
  "fmt"
)

func main() {
  ctx, err := lua.NewContext()
  if err != nil {
    fmt.Printf("%v\n", err)
    return
  }

  if err = ctx.LoadString("res = a + b", map[string]interface{}{
     "a": 10,
     "b": 1,
  }); err == nil {
     res, _ := ctx.GetGlobal("res")
     fmt.Println("result is:", res)
  }
}
```

#### 2. Go calls Lua function

Suppose there's a Lua file named `a.lua` like this:

```lua
function add(a, b)
    return a+b
end
```

one can call the Lua function `add()` in Go code like the following:

```go
package main

import (
  lua "github.com/rosbit/luago"
  "fmt"
)

var add func(int, int)int

func main() {
  ctx, err := lua.NewContext()
  if err != nil {
     fmt.Printf("%v\n", err)
     return
  }

  if err = ctx.LoadFile("a.lua", nil); err != nil {
     fmt.Printf("%v\n", err)
     return
  }

  // method 1: bind Lua function with a golang var
  if err := ctx.BindFunc("add", &add); err != nil {
     fmt.Printf("%v\n", err)
     return
  }
  res := add(1, 2)

  // method 2: call Lua function using CallFunc
  res, err := ctx.CallFunc("add", 1, 2)
  if err != nil {
     fmt.Printf("%v\n", err)
     return
  }

  fmt.Println("result is:", res)
}
```

#### 3. Lua calls Go function

Lua calling Go function is also easy. In the Go code, calling `LoadFile` with a map as env will
make Golang functions as Lua global functions. There's the example:

```go
package main

import "github.com/rosbit/luago"

// function to be called by Lua
func adder(a1 float64, a2 float64) float64 {
    return a1 + a2
}

func main() {
  ctx, err := lua.NewContext()
  if err != nil {
      fmt.Printf("%v\n", err)
      return
  }

  if err := ctx.LoadFile("b.lua", map[string]interface{}{
      "adder": adder,
  })  // b.lua containing code calling "adder"
}
```

In Lua code, one can call the registered function directly. There's the example `b.lua`.

```lua
r = adder(1, 100)   -- the function "adder" is implemented in Go
print(r)
```

### Status

The package is not fully tested, so be careful.

### Contribution

Pull requests are welcome! Also, if you want to discuss something send a pull request with proposal and changes.

__Convention:__ fork the repository and make changes on your fork in a feature branch.
