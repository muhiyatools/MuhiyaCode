package gateway

import (
	"encoding/json"
	"strconv"
)

func modelFloat(item, info map[string]any, key string) float64 {
	value := item[key]
	if value == nil && info != nil {
		value = info[key]
	}
	switch number := value.(type) {
	case float64:
		return number
	case float32:
		return float64(number)
	case int:
		return float64(number)
	case json.Number:
		result, _ := number.Float64()
		return result
	case string:
		result, _ := strconv.ParseFloat(number, 64)
		return result
	default:
		return 0
	}
}
