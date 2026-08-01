package job

import (
	"encoding/json"
	"fmt"
)

func validateProfileConfig(config map[string]any) error {
	if err := validateConfigKeys(config,
		"sampling", "includeCommonValues", "maxCommonValues", "quantiles",
		"distinctStrategy", "maxDistinctValues", "maxMemoryBytes", "sensitiveColumns",
	); err != nil {
		return err
	}
	if raw, ok := config["sampling"]; ok {
		sampling, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("sampling must be an object")
		}
		if err := validateConfigKeys(sampling, "strategy", "maxRows", "seed"); err != nil {
			return err
		}
		if strategy, present := sampling["strategy"]; present {
			if strategy != "RESERVOIR" {
				return fmt.Errorf("sampling.strategy must be RESERVOIR")
			}
		}
		if raw, present := sampling["maxRows"]; present {
			if err := validateIntBound(raw, 0, 100000, "sampling.maxRows"); err != nil {
				return err
			}
		}
		if raw, present := sampling["seed"]; present {
			if err := validateIntBound(raw, 0, 2147483647, "sampling.seed"); err != nil {
				return err
			}
		}
	}
	if raw, ok := config["includeCommonValues"]; ok {
		if _, valid := raw.(bool); !valid {
			return fmt.Errorf("includeCommonValues must be boolean")
		}
	}
	if raw, ok := config["maxCommonValues"]; ok {
		if err := validateIntBound(raw, 0, 100, "maxCommonValues"); err != nil {
			return err
		}
	}
	if raw, ok := config["quantiles"]; ok {
		if err := validateQuantiles(raw); err != nil {
			return err
		}
	}
	if raw, ok := config["distinctStrategy"]; ok {
		strategy, valid := raw.(string)
		if !valid || (strategy != "EXACT" && strategy != "APPROXIMATE") {
			return fmt.Errorf("distinctStrategy must be EXACT or APPROXIMATE")
		}
	}
	if raw, ok := config["maxDistinctValues"]; ok {
		if err := validateIntBound(raw, 128, 1000000, "maxDistinctValues"); err != nil {
			return err
		}
	}
	if raw, ok := config["maxMemoryBytes"]; ok {
		if err := validateIntBound(raw, 1<<20, 1<<32, "maxMemoryBytes"); err != nil {
			return err
		}
	}
	if raw, ok := config["sensitiveColumns"]; ok {
		if err := validateOptionalColumns(raw, "sensitiveColumns"); err != nil {
			return err
		}
	}
	return nil
}

func validateQuantiles(raw any) error {
	values, ok := raw.([]any)
	if !ok {
		if strings, valid := raw.([]float64); valid {
			values = make([]any, len(strings))
			for index := range strings {
				values[index] = strings[index]
			}
		} else {
			return fmt.Errorf("quantiles must be an array")
		}
	}
	if len(values) < 1 || len(values) > 32 {
		return fmt.Errorf("quantiles must contain between 1 and 32 values")
	}
	seen := make(map[float64]struct{}, len(values))
	for _, value := range values {
		number, ok := numericValue(value)
		if !ok || number < 0 || number > 1 {
			return fmt.Errorf("quantiles must contain numbers between 0 and 1")
		}
		if _, duplicate := seen[number]; duplicate {
			return fmt.Errorf("quantiles must not contain duplicates")
		}
		seen[number] = struct{}{}
	}
	return nil
}

func validateOptionalColumns(raw any, name string) error {
	values, ok := raw.([]any)
	if !ok {
		if strings, valid := raw.([]string); valid {
			values = make([]any, len(strings))
			for index := range strings {
				values[index] = strings[index]
			}
		} else {
			return fmt.Errorf("%s must be an array", name)
		}
	}
	if len(values) > 256 {
		return fmt.Errorf("%s must contain at most 256 columns", name)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		column, ok := value.(string)
		if !ok || !validColumnName(column) {
			return fmt.Errorf("%s contains an invalid column", name)
		}
		if _, duplicate := seen[column]; duplicate {
			return fmt.Errorf("%s contains duplicate column %q", name, column)
		}
		seen[column] = struct{}{}
	}
	return nil
}

func validateIntBound(raw any, minimum, maximum int64, name string) error {
	value, ok := integerValue(raw)
	if !ok || value < minimum || value > maximum {
		return fmt.Errorf("%s is outside the supported bound", name)
	}
	return nil
}

func integerValue(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int:
		return int64(value), true
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		return int64(value), uint64(value) <= uint64(^uint64(0)>>1)
	case uint64:
		return int64(value), value <= uint64(^uint64(0)>>1)
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	case float64:
		return int64(value), value == float64(int64(value))
	default:
		return 0, false
	}
}

func numericValue(raw any) (float64, bool) {
	switch value := raw.(type) {
	case float32:
		return float64(value), true
	case float64:
		return value, true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	case json.Number:
		parsed, err := value.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
