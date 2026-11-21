package transitions

import "github.com/soulteary/yunqi/internal/statelog"

func LogStateTransition(component, scope, subject, from, to, cause string, extras ...string) {
	statelog.LogStateTransition(component, scope, subject, from, to, cause, extras...)
}
