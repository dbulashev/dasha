package mapstruct

func SliceMap[T any, U any](slice []T, fn func(T) U) []U {
	result := make([]U, len(slice))
	for i, item := range slice {
		result[i] = fn(item)
	}

	return result
}

func SliceMapWithErr[T any, U any](slice []T, fn func(T) (U, error)) ([]U, error) {
	result := make([]U, len(slice))
	for i, item := range slice {
		res, err := fn(item)
		if err != nil {
			return nil, err
		}

		result[i] = res
	}

	return result, nil
}

func SliceUniqueMember[T any, U comparable](slice []T, fn func(T) U) []U {
	resultSet := make(map[U]bool, len(slice))

	var ret []U

	for _, item := range slice {
		v := fn(item)
		if _, ok := resultSet[v]; !ok {
			resultSet[v] = true
			ret = append(ret, v)
		}
	}

	return ret
}
