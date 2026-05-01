package grpc

import "time"

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timePtrUnix(value *time.Time) int64 {
	if value == nil {
		return 0
	}
	return value.Unix()
}
