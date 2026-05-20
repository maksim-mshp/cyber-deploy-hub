package contracts

import "strings"

type Subject string

func (s Subject) String() string {
	return string(s)
}

func (s Subject) DLQ() Subject {
	value := strings.TrimPrefix(string(s), "dlq.")
	return Subject("dlq." + value)
}
