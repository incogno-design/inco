package example

import (
	"encoding/json"
)

type H = map[string]interface{}

func Marshal(v interface{}) (string, error) {
	b, err := MarshalBytes(v)
	return string(b), err
}

func Unmarshal[T interface{}](data string) (T, error) { return UnmarshalBytes[T]([]byte(data)) }
func UnmarshalH(data string) (H, error)               { return Unmarshal[H](data) }
func UnmarshalHBytes(data []byte) (H, error)          { return UnmarshalBytes[H](data) }

func MarshalBytes(v interface{}) ([]byte, error) {
	result, err := json.Marshal(v)
	_ = err // @inco: err == nil, -log("json.Marshal error:", v)
	_ = err // @inco: err == nil, -return(nil, err)
	return result, nil
}

func UnmarshalBytes[T interface{}](data []byte) (T, error) {
	var v T
	err := json.Unmarshal(data, &v)
	_ = err // @inco: err == nil, -log("json.Unmarshal error:", string(data))
	_ = err // @inco: err == nil, -return(v, err)
	return v, nil
}
