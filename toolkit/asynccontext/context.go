package asynccontext

import (
	"context"
	"reflect"
)

type valueCtxKey struct{}

// WithValue behaves like context.WithValue, with the addition that the
// key/value pair will be copied to the async context when running an
// async operation with Run()
func WithValue(parent context.Context, key, val any) context.Context {
	if key == nil {
		panic("nil key")
	}
	if !reflect.TypeOf(key).Comparable() {
		panic("key is not comparable")
	}
	return &valueCtx{parent, key, val}
}

// A valueCtx carries a key-value pair. It implements Value for that key and
// delegates all other calls to the embedded Context.
type valueCtx struct {
	context.Context
	key, val any
}

type stringer interface {
	String() string
}

// stringify tries a bit to stringify v, without using fmt, since we don't
// want context depending on the unicode tables. This is only used by
// *valueCtx.String().
func stringify(v any) string {
	switch s := v.(type) {
	case stringer:
		return s.String()
	case string:
		return s
	}
	return "<not Stringer>"
}

func (c *valueCtx) String() string {
	return contextName(c.Context) + ".AsyncWithValue(type " +
		reflect.TypeOf(c.key).String() +
		", val " + stringify(c.val) + ")"
}

func (c *valueCtx) Value(key any) any {
	if key == c.key {
		return c.val
	}
	if key == (valueCtxKey{}) {
		return c
	}
	return c.Context.Value(key)
}

func contextName(c context.Context) string {
	if s, ok := c.(stringer); ok {
		return s.String()
	}
	return reflect.TypeOf(c).String()
}

// Clone returns a new context inheriting from context.Background
// that only contains the values set with asynccontext.WithValue.
func Clone(ctx context.Context) context.Context {
	if val := ctx.Value(valueCtxKey{}); val != nil {
		val := val.(*valueCtx)

		innerCtx := Clone(val.Context)
		return WithValue(innerCtx, val.key, val.val)
	}
	return context.Background()
}
