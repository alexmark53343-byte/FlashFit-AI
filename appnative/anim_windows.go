//go:build windows

package main

import (
	"math"
	"unsafe"
)

// One clock and one scheduler for every animation in the window.
//
// The rule this file exists to enforce: animation state advances by *elapsed
// time*, never by frame count. A counter incremented once per paint ties the
// speed of every effect to how fast the machine happens to be painting, so the
// moment something heavy runs — mesh analysis, a slicer launch — every
// animation slows down with it instead of staying at its real rate.
//
// Frame-time budget for the whole scheduler: < 0.2 ms per tick on the
// reference machine. It only walks a fixed-size array of floats; anything that
// needs to rasterise or measure geometry does not belong here.

var (
	pQueryPerformanceCounter   = kernel32.NewProc("QueryPerformanceCounter")
	pQueryPerformanceFrequency = kernel32.NewProc("QueryPerformanceFrequency")
)

type animationClock struct {
	frequency int64
	last      int64
	// elapsed is wall-clock seconds since the clock started, and is what every
	// continuous effect derives its phase from.
	elapsed float64
}

var animClock animationClock

func nowCounter() int64 {
	var value int64
	pQueryPerformanceCounter.Call(uintptr(unsafe.Pointer(&value)))
	return value
}

func (c *animationClock) start() {
	if c.frequency == 0 {
		var freq int64
		pQueryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&freq)))
		if freq <= 0 {
			freq = 1
		}
		c.frequency = freq
	}
	c.last = nowCounter()
}

// tick returns the seconds elapsed since the previous tick. The result is
// clamped: a window that was minimised, dragged or blocked for a second must
// not make everything leap forward when it comes back.
func (c *animationClock) tick() float64 {
	if c.frequency == 0 {
		c.start()
		return 0
	}
	now := nowCounter()
	delta := float64(now-c.last) / float64(c.frequency)
	c.last = now
	if delta < 0 || delta > 0.25 {
		delta = 0.016
	}
	c.elapsed += delta
	return delta
}

func animElapsed() float64 { return animClock.elapsed }

// animPhase returns a value that walks 0..1 every period seconds, for effects
// that loop: a shimmer sweep, a travelling gradient.
func animPhase(period float64) float64 {
	if period <= 0 {
		return 0
	}
	return math.Mod(animClock.elapsed, period) / period
}

// animPulse returns a smooth 0..1..0 over the period, for anything that
// breathes rather than sweeps.
func animPulse(period float64) float64 {
	return 0.5 - 0.5*math.Cos(animPhase(period)*2*math.Pi)
}

// approachRate moves a value toward a target at a rate expressed in "fraction
// of the remaining distance per second", so the easing is identical whether the
// window is painting at 60 fps or struggling at 12.
//
// The exponential form is what makes it frame-rate independent: applying it
// twice for dt/2 gives the same result as once for dt.
func approachRate(current, target, ratePerSecond, dt float64) float64 {
	if dt <= 0 || ratePerSecond <= 0 {
		return current
	}
	blend := 1 - math.Exp(-ratePerSecond*dt)
	next := current + (target-current)*blend
	// Snap when close enough, so a track can actually finish and let the
	// scheduler go idle instead of easing forever.
	if math.Abs(target-next) < 0.002 {
		return target
	}
	return next
}

// easeOutCubic is the shared curve for discrete transitions.
func easeOutCubic(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	inv := 1 - t
	return 1 - inv*inv*inv
}
