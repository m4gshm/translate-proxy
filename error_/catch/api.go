package catch

import "github.com/m4gshm/expressions/error_"

func Two[F, S any](first F, second S, err error) (error_.Catcher, F, S) {
	return error_.Catcher{Err: err}, first, second
}

func One[T any](result T, err error) (error_.Catcher, T) {
	return error_.Catcher{Err: err}, result
}
