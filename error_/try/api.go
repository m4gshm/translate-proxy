package try

import "github.com/m4gshm/expressions/error_"

func Get[T any](catch error_.Catcher, routine func() T) (out T) {
	if catch.Err == nil {
		return routine()
	}
	return out
}

func GetCatch[T any](catch error_.Catcher, routine func() (T, error)) (out T) {
	if catch.Err != nil {
		return out
	}
	out, catch.Err = routine()
	return out
}

func ConvertCatch[I, O any](catch error_.Catcher, element I, converter func(I) (O, error)) (out O) {
	if catch.Err != nil {
		return out
	}
	out, catch.Err = converter(element)
	return out
}

func Convert[I, O any](catch error_.Catcher, element I, converter func(I) O) (out O) {
	if catch.Err != nil {
		return out
	}
	return converter(element)
}
