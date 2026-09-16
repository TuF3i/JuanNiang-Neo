package models

import (
	"database/sql/driver"
	"encoding/json"
)

// ---------- 通用 JSON 字段类型 ----------

// JSONMap 是 map[string]any 的 GORM 兼容类型。
type JSONMap map[string]any

func (j JSONMap) Value() (driver.Value, error) {
	if j == nil {
		return "{}", nil
	}
	b, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (j *JSONMap) Scan(value any) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, j)
}

// JSONSlice 是 []string 的 GORM 兼容类型。
type JSONSlice []string

func (j JSONSlice) Value() (driver.Value, error) {
	if j == nil {
		return "[]", nil
	}
	b, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (j *JSONSlice) Scan(value any) error {
	if value == nil {
		return nil
	}
	// 兼容 []byte（Postgres jsonb）与 string（SQLite 文本列）
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return nil
	}
	return json.Unmarshal(b, j)
}

// Int64Slice 是 []int64 的 GORM 兼容类型（加群审核生效群列表等数值 ID 集合）。
type Int64Slice []int64

func (s Int64Slice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (s *Int64Slice) Scan(value any) error {
	if value == nil {
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return nil
	}
	return json.Unmarshal(b, s)
}

// StringMap 是 map[string]string 的 GORM 兼容类型（加群审核每群提示词，键为群号字符串）。
type StringMap map[string]string

func (m StringMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (m *StringMap) Scan(value any) error {
	if value == nil {
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return nil
	}
	return json.Unmarshal(b, m)
}
