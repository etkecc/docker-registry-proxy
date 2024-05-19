package apm

import (
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/rs/zerolog"
)

// Healthchecks is the interface for gitlab.com/etke.cc/go/healthchecks,
// to avoid direct dependecy on the package for project where it is not needed
type Healthchecks interface {
	Fail(optionalBody ...io.Reader)
}

// healthchecksHook is a hook for zerolog that sends a healthcheck fail event on error
type healthchecksHook struct{}

var (
	hcHook       zerolog.Hook = healthchecksHook{}
	hcHookLevels              = map[zerolog.Level]struct{}{ //nolint:exhaustive // that's the point
		zerolog.ErrorLevel: {},
		zerolog.FatalLevel: {},
		zerolog.PanicLevel: {},
	}
)

func (h healthchecksHook) Run(e *zerolog.Event, level zerolog.Level, msg string) {
	// only send healthchecks on error levels
	if _, ok := hcHookLevels[level]; !ok {
		return
	}

	// if the message is empty, we don't want to send a healthcheck
	if msg == "" {
		return
	}

	// create a string that appends } to the end of the buf variable you access via reflection, to get a valid JSON object
	// ref: https://github.com/rs/zerolog/issues/493#issuecomment-1458241050
	ev := fmt.Sprintf("%s}", reflect.ValueOf(e).Elem().FieldByName("buf"))

	HealthcheckFail(strings.NewReader(msg + " " + ev))
}

// HealthcheckFail sends a healthcheck fail event (if healthchecks are configured)
func HealthcheckFail(optionalBody ...io.Reader) {
	if hc != nil {
		hc.Fail(optionalBody...)
	}
}
