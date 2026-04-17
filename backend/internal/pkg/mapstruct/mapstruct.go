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

// JoinPair holds the result of a full outer join for a single key.
type JoinPair[T1, T2 any] struct {
	Left  *T1
	Right *T2
}

// SliceFullJoin performs a full outer join of two slices by key.
func SliceFullJoin[T1, T2 any, K comparable](
	left []T1,
	right []T2,
	leftKey func(T1) K,
	rightKey func(T2) K,
) []JoinPair[T1, T2] {
	leftIndex := make(map[K]*T1, len(left))
	rightIndex := make(map[K]*T2, len(right))

	for i := range left {
		leftIndex[leftKey(left[i])] = &left[i]
	}

	for i := range right {
		rightIndex[rightKey(right[i])] = &right[i]
	}

	result := make([]JoinPair[T1, T2], 0, len(left)+len(right))

	for key, lp := range leftIndex {
		if rp, ok := rightIndex[key]; ok {
			result = append(result, JoinPair[T1, T2]{Left: lp, Right: rp})
			delete(rightIndex, key)
		} else {
			result = append(result, JoinPair[T1, T2]{Left: lp, Right: nil})
		}
	}

	for _, rp := range rightIndex {
		result = append(result, JoinPair[T1, T2]{Left: nil, Right: rp})
	}

	return result
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
