// Copyright 2018 Netflix, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package env provides an `env` struct field tag to marshal and unmarshal
// environment variables.
package env

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

var (
	// ErrInvalidValue returned when the value passed to Unmarshal is nil or not a
	// pointer to a struct.
	ErrInvalidValue = errors.New("value must be a non-nil pointer to a struct")

	// ErrUnsupportedType returned when a field with tag "env" is unsupported.
	ErrUnsupportedType = errors.New("field is an unsupported type")

	// ErrUnexportedField returned when a field with tag "env" is not exported.
	ErrUnexportedField = errors.New("field must be exported")
)

// Unmarshal parses an EnvSet and stores the result in the value pointed to by
// v. Fields that are matched in v will be deleted from EnvSet, resulting in
// an EnvSet with the remaining environment variables. If v is nil or not a
// pointer to a struct, Unmarshal returns an ErrInvalidValue.
//
// Fields tagged with "env" will have the unmarshalled EnvSet of the matching
// key from EnvSet. If the tagged field is not exported, Unmarshal returns
// ErrUnexportedField.
//
// If the field has a type that is unsupported, Unmarshal returns
// ErrUnsupportedType.
func Unmarshal(es EnvSet, v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return ErrInvalidValue
	}

	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return ErrInvalidValue
	}

	t := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		valueField := rv.Field(i)
		switch valueField.Kind() {
		case reflect.Struct:
			if !valueField.Addr().CanInterface() {
				continue
			}

			iface := valueField.Addr().Interface()
			err := Unmarshal(es, iface)
			if err != nil {
				return err
			}
		}

		typeField := t.Field(i)
		tag := typeField.Tag.Get("env")
		if tag == "" {
			continue
		}

		if !valueField.CanSet() {
			return ErrUnexportedField
		}

		envVar, ok := es[tag]
		if !ok {
			continue
		}

		err := set(typeField.Type, valueField, envVar)
		if err != nil {
			return err
		}
		delete(es, tag)
	}

	return nil
}

func set(t reflect.Type, f reflect.Value, value string) error {
	switch t.Kind() {
	case reflect.Ptr:
		ptr := reflect.New(t.Elem())
		err := set(t.Elem(), ptr.Elem(), value)
		if err != nil {
			return err
		}
		f.Set(ptr)
	case reflect.String:
		f.SetString(value)
	case reflect.Bool:
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		f.SetBool(v)
	case reflect.Int:
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		f.SetInt(int64(v))
	case reflect.Slice:
		// split the environment variable string and check if it is not empty
		a := strings.Split(value, ",")
		if len(a) == 0 {
			return ErrUnsupportedType
		}

		// create slice based on for defined type
		v := reflect.MakeSlice(t, len(a), len(a))

		// loop through input, parse to required type and add to the slice
		elementType := t.Elem().Kind()
		for index, element := range a {
			switch elementType {
				case reflect.String:
					v.Index(index).Set(reflect.ValueOf(element))
				case reflect.Int:
					elementInt, err := strconv.Atoi(element)
					if err != nil {
						return ErrUnsupportedType
					}
					// f.SetInt(int64(elementInt))
					// v.Elem().SetInt(int64(elementInt))
					v.Index(index).SetInt(int64(elementInt))
				default:
					return ErrUnsupportedType
			}
		}

		// set value
		f.Set(v)

	default:
		return ErrUnsupportedType
	}
	return nil
}

// UnmarshalFromEnviron parses an EnvSet from os.Environ and stores the result
// in the value pointed to by v. Fields that weren't matched in v are returned
// in an EnvSet with the remaining environment variables. If v is nil or not a
// pointer to a struct, UnmarshalFromEnviron returns an ErrInvalidValue.
//
// Fields tagged with "env" will have the unmarshalled EnvSet of the matching
// key from EnvSet. If the tagged field is not exported, UnmarshalFromEnviron
// returns ErrUnexportedField.
//
// If the field has a type that is unsupported, UnmarshalFromEnviron returns
// ErrUnsupportedType.
func UnmarshalFromEnviron(v interface{}) (EnvSet, error) {
	es, err := EnvironToEnvSet(os.Environ())
	if err != nil {
		return nil, err
	}

	return es, Unmarshal(es, v)
}

// Marshal returns an EnvSet of v. If v is nil or not a pointer, Marshal returns
// an ErrInvalidValue.
//
// Marshal uses fmt.Sprintf to transform encountered values to its default
// string format. Values without the "env" field tag are ignored.
//
// Nested structs are traversed recursively.
func Marshal(v interface{}) (EnvSet, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return nil, ErrInvalidValue
	}

	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return nil, ErrInvalidValue
	}

	es := make(EnvSet)
	t := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		valueField := rv.Field(i)
		switch valueField.Kind() {
		case reflect.Slice:
			typeField := t.Field(i)
			tag := typeField.Tag.Get("env")
			if tag == "" {
				continue
			}
			switch valueField.Type().Elem().Kind() {
			case reflect.String:
				slice, ok := valueField.Interface().([]string)
				if !ok {
					return nil, ErrUnsupportedType
				}
				es[tag] = strings.Join(slice, ",")
				continue
			case reflect.Int:
				slice, ok := valueField.Interface().([]int)
				if !ok {
					return nil, ErrUnsupportedType
				}
				b := make([]string, len(slice))
				for i, v := range slice {
					b[i] = strconv.Itoa(v)
				}
				es[tag] = strings.Join(b, ",")
				continue
			default:
				continue
			}
			break
		case reflect.Struct:
			if !valueField.Addr().CanInterface() {
				continue
			}

			iface := valueField.Addr().Interface()
			nes, err := Marshal(iface)
			if err != nil {
				return nil, err
			}

			for k, v := range nes {
				es[k] = v
			}
		}

		typeField := t.Field(i)
		tag := typeField.Tag.Get("env")
		if tag == "" {
			continue
		}

		if typeField.Type.Kind() == reflect.Ptr {
			if valueField.IsNil() {
				continue
			}
			es[tag] = fmt.Sprintf("%v", valueField.Elem().Interface())
		} else {
			es[tag] = fmt.Sprintf("%v", valueField.Interface())
		}
	}

	return es, nil
}
