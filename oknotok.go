package oknotok

import "time"

// default settings
const (
	defaultInterval              = time.Duration(0) * time.Second
	defaultTimeout               = time.Duration(60) * time.Second
	defaultMaxContinuousFailures = 5
)

// Core domain type, implementing the most important features of a
// CircuitBreaker. It helps to protect the environment
// from sending requests that probably are going to fail.
// Source of inspiration: https://martinfowler.com/bliki/CircuitBreaker.html
type OkNotOk struct {
	name              string
	maxHalfOkRequests uint64
	interval          time.Duration
	timeout           time.Duration
	healed            func(stats Stats) bool
	stateChanged      func(name string, from, to CircuitState)
	shouldCountError  func(err error) bool

	state           CircuitState
	stateExpiration time.Time
	stats           Stats
}

// returns a new OkNotOk instance properly configured
func NewOkNotOk(settings Settings) *OkNotOk {
	oknok := OkNotOk{}
	oknok.name = settings.Name
	oknok.stateChanged = settings.StateChanged

	if settings.Interval > 0 {
		oknok.interval = settings.Interval
	} else {
		oknok.interval = defaultInterval
	}

	if settings.MaxHalfOkRequests > 0 {
		oknok.maxHalfOkRequests = settings.MaxHalfOkRequests
	} else {
		oknok.maxHalfOkRequests = 1
	}

	if settings.Timeout > 0 {
		oknok.timeout = settings.Timeout
	} else {
		oknok.timeout = defaultTimeout
	}

	if settings.ShoulCountError != nil {
		oknok.shouldCountError = settings.ShoulCountError
	} else {
		oknok.shouldCountError = defaultShouldCountError
	}

	if settings.Healed != nil {
		oknok.healed = settings.Healed
	} else {
		oknok.healed = defaultHealed
	}

	return &oknok
}

func defaultHealed(stats Stats) bool {
	return stats.continuousFailures > defaultMaxContinuousFailures
}

func defaultShouldCountError(err error) bool {
	return err == nil
}

// trigger a request if the current state of
// OkNotOk allows it. In case of OkNotOk rejection,
// an error will be returned.
// TODO implement PreCall and PostCall
func (ok *OkNotOk) Call(req func() (interface{}, error)) (interface{}, error) {
	result, err := req()
	return result, err
}

// TODO make it thread-safe
func (ok *OkNotOk) preCall() error {
	now := time.Now()
	state := ok.defineCurrentState(now)

	if state == StateNotOk {
		return ErrCircuitNotOk
	}

	if ok.maxCallsCheck(state) {
		return ErrTooManyCalls
	}

	ok.stats.onCall()
	return nil
}

// detects if OkNotOk has reached its max calls config in halfOK state
func (ok *OkNotOk) maxCallsCheck(state CircuitState) bool {
	return state == StateHalfOk && ok.stats.calls >= ok.maxHalfOkRequests
}

// interprets current circuit stats to define its current state
func (ok *OkNotOk) defineCurrentState(now time.Time) CircuitState {
	switch ok.state {
	case StateOk:
		if !ok.stateExpiration.IsZero() && ok.stateExpiration.Before(now) {
			ok.restartClock(now)
		}
	case StateNotOk:
		if ok.stateExpiration.Before(now) {
			ok.setState(StateHalfOk, now)
		}
	}

	return ok.state
}

// sets a new states to the circuit breaker
func (ok *OkNotOk) setState(toState CircuitState, now time.Time) {
	if ok.state == toState {
		// nothing to do...
		return
	}

	// switching states
	fromState := ok.state
	ok.state = toState

	ok.restartClock(now)

	if ok.stateChanged != nil {
		ok.stateChanged(ok.name, fromState, toState)
	}
}

// adjust internal timers and clear internal stats to a now round of calls
func (ok *OkNotOk) restartClock(now time.Time) {
	// clearing stats
	ok.stats.reset()

	// reseting state
	switch ok.state {
	case StateNotOk:
		ok.stateExpiration = now.Add(ok.timeout)
	case StateHalfOk:
		ok.stateExpiration = time.Time{}
	default: // StateOk
		if ok.interval == 0 {
			ok.stateExpiration = time.Time{}
		} else {
			ok.stateExpiration = now.Add(ok.interval)
		}
	}

}
