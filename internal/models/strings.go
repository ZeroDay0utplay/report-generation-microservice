package models

func StringOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func StringPtr(v string) *string {
	return &v
}
