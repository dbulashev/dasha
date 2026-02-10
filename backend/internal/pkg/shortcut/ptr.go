package shortcut

func Ptr[T any](t T) *T {
	ret := t

	return &ret
}
