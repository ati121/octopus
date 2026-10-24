package axon

import (
	"bytes"
	"net/url"
	"reflect"
	"slices"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func cloneNativeRequest(src *llm.Request) *llm.Request {
	value := *src
	// RawRequest can reference a live HTTP request. Clone only the transport
	// payload below, not contexts, callbacks or connections.
	value.RawRequest = nil
	cloned := cloneRequestValue(reflect.ValueOf(value)).Interface().(llm.Request)
	if raw := src.RawRequest; raw != nil {
		cloned.RawRequest = &httpclient.Request{
			Method: raw.Method, URL: raw.URL, Path: raw.Path,
			Query: cloneRequestQuery(raw.Query), Headers: raw.Headers.Clone(), ContentType: raw.ContentType,
			Body: bytes.Clone(raw.Body), JSONBody: bytes.Clone(raw.JSONBody),
			RequestID: raw.RequestID, ClientIP: raw.ClientIP, UserAgent: raw.UserAgent,
			RequestType: raw.RequestType, APIFormat: raw.APIFormat,
		}
		if raw.Auth != nil {
			auth := *raw.Auth
			cloned.RawRequest.Auth = &auth
		}
	}
	return &cloned
}

func cloneRequestQuery(src url.Values) url.Values {
	if src == nil {
		return nil
	}
	dst := make(url.Values, len(src))
	for key, values := range src {
		dst[key] = slices.Clone(values)
	}
	return dst
}

// AxonHub payloads are value structs and JSON-shaped trees. Copy their mutable
// containers while sharing immutable strings, so Base64 is never re-encoded.
func cloneRequestValue(src reflect.Value) reflect.Value {
	switch src.Kind() {
	case reflect.Pointer, reflect.Interface:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		if src.Kind() == reflect.Pointer {
			dst := reflect.New(src.Type().Elem())
			dst.Elem().Set(cloneRequestValue(src.Elem()))
			return dst
		}
		dst := reflect.New(src.Type()).Elem()
		dst.Set(cloneRequestValue(src.Elem()))
		return dst
	case reflect.Slice:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		dst := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		if src.Type().Elem().Kind() == reflect.Uint8 {
			reflect.Copy(dst, src)
		} else {
			for i := 0; i < src.Len(); i++ {
				dst.Index(i).Set(cloneRequestValue(src.Index(i)))
			}
		}
		return dst
	case reflect.Map:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		dst := reflect.MakeMapWithSize(src.Type(), src.Len())
		iter := src.MapRange()
		for iter.Next() {
			dst.SetMapIndex(iter.Key(), cloneRequestValue(iter.Value()))
		}
		return dst
	case reflect.Struct:
		dst := reflect.New(src.Type()).Elem()
		dst.Set(src)
		for i := 0; i < src.NumField(); i++ {
			if src.Type().Field(i).IsExported() {
				dst.Field(i).Set(cloneRequestValue(src.Field(i)))
			}
		}
		return dst
	case reflect.Array:
		dst := reflect.New(src.Type()).Elem()
		for i := 0; i < src.Len(); i++ {
			dst.Index(i).Set(cloneRequestValue(src.Index(i)))
		}
		return dst
	default:
		return src
	}
}
