package helpers

func DerefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
